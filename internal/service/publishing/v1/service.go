// Package publishing connects a workspace's YouTube channel and blog, and
// publishes approved deliverables to them.
package publishing

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"ekin/internal/adapter/github"
	"ekin/internal/adapter/youtube"
	"ekin/internal/apperror"
	pipelinedomain "ekin/internal/domain/pipeline/v1"
	publishingdomain "ekin/internal/domain/publishing/v1"
	storydomain "ekin/internal/domain/story/v1"
	publishingrepo "ekin/internal/repository/publishing/v1"
	productionsvc "ekin/internal/service/production/v1"
	"ekin/pkg/secretbox"
)

// stateTTL bounds how long an OAuth round trip may take.
const stateTTL = 15 * time.Minute

// Service runs publishing use cases.
type Service struct {
	repo publishingrepo.Repository
	box  *secretbox.Box
	yt   func(ctx context.Context) *youtube.Client
	gh   *github.Client
	log  *slog.Logger
	now  func() time.Time
}

// NewService returns a Service. yt returns the OAuth client with the
// current credentials (they can change from the settings page).
func NewService(repo publishingrepo.Repository, box *secretbox.Box, yt func(ctx context.Context) *youtube.Client, gh *github.Client, log *slog.Logger) *Service {
	return &Service{repo: repo, box: box, yt: yt, gh: gh, log: log, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// YouTubeConfigured reports whether OAuth credentials exist.
func (s *Service) YouTubeConfigured(ctx context.Context) bool { return s.yt(ctx).Configured() }

type oauthState struct {
	WorkspaceID uuid.UUID `json:"w"`
	UserID      uuid.UUID `json:"u"`
	Expires     int64     `json:"e"`
}

// ConnectURL starts connecting a YouTube channel to the workspace.
func (s *Service) ConnectURL(ctx context.Context, workspaceID, userID uuid.UUID) (string, error) {
	if !s.yt(ctx).Configured() {
		return "", apperror.New(apperror.ValidationFailed, "YouTube is not set up on this server (EKIN_YOUTUBE_CLIENT_ID and EKIN_YOUTUBE_CLIENT_SECRET)")
	}
	raw, _ := json.Marshal(oauthState{WorkspaceID: workspaceID, UserID: userID, Expires: s.now().Add(stateTTL).Unix()})
	return s.yt(ctx).AuthURL(s.box.Sign(string(raw))), nil
}

// Callback finishes the OAuth round trip and stores the channel. It
// returns the workspace the flow started from.
func (s *Service) Callback(ctx context.Context, state, code string) (uuid.UUID, error) {
	raw, err := s.box.Verify(state)
	if err != nil {
		return uuid.Nil, apperror.New(apperror.BadRequest, "the connection request is invalid; start again")
	}
	var st oauthState
	if err := json.Unmarshal([]byte(raw), &st); err != nil || s.now().Unix() > st.Expires {
		return st.WorkspaceID, apperror.New(apperror.BadRequest, "the connection request expired; start again")
	}
	refresh, access, err := s.yt(ctx).Exchange(ctx, code)
	if err != nil {
		return st.WorkspaceID, err
	}
	id, title, err := s.yt(ctx).Channel(ctx, access)
	if err != nil {
		return st.WorkspaceID, err
	}
	ch, err := publishingdomain.NewChannel(st.WorkspaceID, id, title, s.box.Seal(refresh), st.UserID, s.now())
	if err != nil {
		return st.WorkspaceID, err
	}
	if old, err := s.repo.Channel(ctx, st.WorkspaceID); err == nil && old.ChannelID == id {
		ch.Privacy = old.Privacy
	}
	return st.WorkspaceID, s.repo.PutChannel(ctx, ch)
}

// StateWorkspace returns the workspace a signed OAuth state was issued
// for, or Nil when the state is not ours. Expiry is not checked: it only
// decides where to send the browser after Google reported an error.
func (s *Service) StateWorkspace(state string) uuid.UUID {
	raw, err := s.box.Verify(state)
	if err != nil {
		return uuid.Nil
	}
	var st oauthState
	if json.Unmarshal([]byte(raw), &st) != nil {
		return uuid.Nil
	}
	return st.WorkspaceID
}

// Channel returns the connected channel, if any.
func (s *Service) Channel(ctx context.Context, workspaceID uuid.UUID) (publishingdomain.Channel, bool, error) {
	ch, err := s.repo.Channel(ctx, workspaceID)
	if apperror.Is(err, apperror.NotFound) {
		return ch, false, nil
	}
	return ch, err == nil, err
}

// SetPrivacy changes what uploads are published as.
func (s *Service) SetPrivacy(ctx context.Context, workspaceID uuid.UUID, p publishingdomain.Privacy) (publishingdomain.Channel, error) {
	ch, err := s.repo.Channel(ctx, workspaceID)
	if err != nil {
		return ch, err
	}
	ch.Privacy, ch.UpdatedAt = p, s.now()
	return ch, s.repo.PutChannel(ctx, ch)
}

// Disconnect forgets the channel.
func (s *Service) Disconnect(ctx context.Context, workspaceID uuid.UUID) error {
	return s.repo.DeleteChannel(ctx, workspaceID)
}

// BlogInput configures the blog; a nil Token keeps the stored one.
type BlogInput struct {
	publishingdomain.BlogTarget
	Token *string
}

// Blog returns the blog target, if any.
func (s *Service) Blog(ctx context.Context, workspaceID uuid.UUID) (publishingdomain.BlogTarget, bool, error) {
	b, err := s.repo.Blog(ctx, workspaceID)
	if apperror.Is(err, apperror.NotFound) {
		return b, false, nil
	}
	return b, err == nil, err
}

// SaveBlog validates and stores the blog target.
func (s *Service) SaveBlog(ctx context.Context, workspaceID uuid.UUID, in BlogInput) (publishingdomain.BlogTarget, error) {
	b := in.BlogTarget
	b.WorkspaceID, b.UpdatedAt = workspaceID, s.now()
	if in.Token != nil {
		b.SealedToken = ""
		if *in.Token != "" {
			b.SealedToken = s.box.Seal(*in.Token)
		}
	} else if old, err := s.repo.Blog(ctx, workspaceID); err == nil {
		b.SealedToken = old.SealedToken
	}
	if err := b.Validate(); err != nil {
		return b, err
	}
	if b.Kind == publishingdomain.BlogDir {
		if st, err := os.Stat(b.Dir); err != nil || !st.IsDir() {
			return b, apperror.New(apperror.ValidationFailed, "the checkout directory does not exist on the server")
		}
	}
	return b, s.repo.PutBlog(ctx, b)
}

// DeleteBlog forgets the blog target.
func (s *Service) DeleteBlog(ctx context.Context, workspaceID uuid.UUID) error {
	return s.repo.DeleteBlog(ctx, workspaceID)
}

// Video returns the YouTube publisher.
func (s *Service) Video() productionsvc.Publisher { return videoPublisher{s} }

// Article returns the blog publisher.
func (s *Service) Article() productionsvc.Publisher { return articlePublisher{s} }

type videoPublisher struct{ s *Service }

func (p videoPublisher) Kind() pipelinedomain.Kind { return pipelinedomain.KindVideo }

func (p videoPublisher) Ready(ctx context.Context, ws uuid.UUID) (bool, error) {
	if !p.s.yt(ctx).Configured() {
		return false, nil
	}
	_, ok, err := p.s.Channel(ctx, ws)
	return ok, err
}

type metadata struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Chapters    []struct {
		At    float64 `json:"at"`
		Title string  `json:"title"`
	} `json:"chapters"`
}

func readAll(ctx context.Context, req productionsvc.PublishRequest, name string) ([]byte, error) {
	rc, _, err := req.Open(ctx, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, 16<<20))
}

func (p videoPublisher) Publish(ctx context.Context, req productionsvc.PublishRequest) (storydomain.Publication, error) {
	s := p.s
	ch, err := s.repo.Channel(ctx, req.WorkspaceID)
	if err != nil {
		return storydomain.Publication{}, err
	}
	refresh, err := s.box.Open(ch.SealedToken)
	if err != nil {
		return storydomain.Publication{}, err
	}
	yt := s.yt(ctx)
	access, err := yt.Access(ctx, refresh)
	if err != nil {
		return storydomain.Publication{}, fmt.Errorf("%w (reconnect the channel in workspace settings)", err)
	}
	raw, err := readAll(ctx, req, storydomain.ArtifactMetadata)
	if err != nil {
		return storydomain.Publication{}, err
	}
	var meta metadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return storydomain.Publication{}, fmt.Errorf("metadata.json: %w", err)
	}
	description := meta.Description
	if len(meta.Chapters) > 1 {
		var b strings.Builder
		b.WriteString(description + "\n\n")
		for _, c := range meta.Chapters {
			fmt.Fprintf(&b, "%s %s\n", timestamp(c.At), c.Title)
		}
		description = strings.TrimSpace(b.String())
	}
	video, size, err := req.Open(ctx, storydomain.ArtifactFinal)
	if err != nil {
		return storydomain.Publication{}, err
	}
	defer func() { _ = video.Close() }()
	id, err := yt.Upload(ctx, access, youtube.Video{
		Title: meta.Title, Description: description, Tags: meta.Tags, Language: string(req.Deliverable.Language()),
		Privacy: string(ch.Privacy), Body: video, Size: size,
	})
	if err != nil {
		return storydomain.Publication{}, err
	}
	thumbnail := cmp.Or(req.Thumbnail, storydomain.ArtifactPoster)
	if image, _, err := req.Open(ctx, thumbnail); err == nil {
		if err := yt.Thumbnail(ctx, access, id, image); err != nil {
			s.log.InfoContext(ctx, "custom thumbnail not set", slog.String("thumbnail", thumbnail), slog.Any("error", err))
			if req.Warn != nil && thumbnail != storydomain.ArtifactPoster {
				req.Warn("YouTube did not take the chosen thumbnail (an unverified channel cannot set custom thumbnails); " +
					"upload it in YouTube Studio from the story's thumbnail downloads")
			}
		}
		_ = image.Close()
	}
	url := "https://youtu.be/" + id
	if v := req.Deliverable.Video(); v != nil && v.Format() == pipelinedomain.FormatShort {
		url = "https://www.youtube.com/shorts/" + id
	}
	return storydomain.Publication{URL: url, ExternalID: id}, nil
}

func timestamp(sec float64) string {
	t := int(sec)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

type articlePublisher struct{ s *Service }

func (p articlePublisher) Kind() pipelinedomain.Kind { return pipelinedomain.KindArticle }

func (p articlePublisher) Ready(ctx context.Context, ws uuid.UUID) (bool, error) {
	_, ok, err := p.s.Blog(ctx, ws)
	return ok, err
}

var embedMarker = regexp.MustCompile(`(?m)^\s*<YouTube\s+id="([A-Za-z0-9_-]{6,20})"\s*/>\s*$`)

// frontmatter is the marketing site's blog schema.
type frontmatter struct {
	Title          string   `yaml:"title"`
	SEOTitle       string   `yaml:"seoTitle,omitempty"`
	Description    string   `yaml:"description"`
	PubDate        string   `yaml:"pubDate"`
	Locale         string   `yaml:"locale"`
	Draft          bool     `yaml:"draft"`
	Author         string   `yaml:"author,omitempty"`
	Tags           []string `yaml:"tags"`
	TranslationKey string   `yaml:"translationKey,omitempty"`
}

func (p articlePublisher) Publish(ctx context.Context, req productionsvc.PublishRequest) (storydomain.Publication, error) {
	s := p.s
	b, err := s.repo.Blog(ctx, req.WorkspaceID)
	if err != nil {
		return storydomain.Publication{}, err
	}
	raw, err := readAll(ctx, req, storydomain.ArtifactArticle)
	if err != nil {
		return storydomain.Publication{}, err
	}
	a, err := productionsvc.ParseArticle(raw)
	if err != nil {
		return storydomain.Publication{}, err
	}
	locale := string(req.Deliverable.Language())
	fm := frontmatter{
		Title: a.Title, SEOTitle: a.SEOTitle, Description: a.Description, PubDate: s.now().Format("2006-01-02"),
		Locale: locale, Draft: b.Draft, Author: b.Author, Tags: a.Tags, TranslationKey: req.TranslationKey,
	}
	head, err := yaml.Marshal(fm)
	if err != nil {
		return storydomain.Publication{}, err
	}
	vertical := req.Lead != nil && strings.Contains(req.Lead.URL, "/shorts/")
	body := embedMarker.ReplaceAllStringFunc(a.Body, func(m string) string {
		return embed(embedMarker.FindStringSubmatch(m)[1], a.Title, vertical)
	})
	post := "---\n" + string(head) + "---\n\n" + body + "\n"
	path := filepath.ToSlash(filepath.Join(b.ContentDir, locale, a.Slug+".md"))
	files := []github.File{{Path: path, Content: []byte(post)}}

	switch b.Kind {
	case publishingdomain.BlogGitHub:
		token, err := s.box.Open(b.SealedToken)
		if err != nil {
			return storydomain.Publication{}, err
		}
		if _, err := s.gh.Commit(ctx, token, b.Repo, b.Branch, "blog: "+a.Title, files); err != nil {
			return storydomain.Publication{}, err
		}
	case publishingdomain.BlogDir:
		for _, f := range files {
			target := filepath.Join(b.Dir, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return storydomain.Publication{}, err
			}
			if err := os.WriteFile(target, f.Content, 0o600); err != nil {
				return storydomain.Publication{}, err
			}
		}
	default:
		return storydomain.Publication{}, errors.New("unknown blog target kind")
	}
	url := b.PostURL(locale, a.Slug)
	if b.SiteURL == "" {
		url = "file://" + path
	}
	return storydomain.Publication{URL: url, ExternalID: a.Slug}, nil
}

// embed is a privacy-friendly responsive YouTube player in plain HTML,
// valid in .md and .mdx alike.
func embed(id, title string, vertical bool) string {
	ratio, width := "56.25%", "100%"
	if vertical {
		ratio, width = "177.78%", "min(100%, 360px)"
	}
	title = strings.ReplaceAll(title, `"`, "&quot;")
	return fmt.Sprintf(`<div style="position:relative;width:%s;margin:2rem auto"><div style="position:relative;padding-top:%s">`+
		`<iframe src="https://www.youtube-nocookie.com/embed/%s" title="%s" loading="lazy" allowfullscreen `+
		`allow="accelerometer; encrypted-media; gyroscope; picture-in-picture" `+
		`style="position:absolute;inset:0;width:100%%;height:100%%;border:0;border-radius:12px"></iframe></div></div>`,
		width, ratio, id, title)
}
