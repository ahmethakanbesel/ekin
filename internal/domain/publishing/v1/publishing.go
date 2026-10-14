// Package publishing models where a workspace publishes: its YouTube
// channel and its blog (the marketing site's content collection in git).
package publishing

import (
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/pkg/timeutil"
)

// Privacy is a YouTube video's visibility after upload.
type Privacy string

const (
	PrivacyPrivate  Privacy = "private"
	PrivacyUnlisted Privacy = "unlisted"
	PrivacyPublic   Privacy = "public"
)

// ParsePrivacy parses a privacy setting.
func ParsePrivacy(s string) (Privacy, error) {
	switch p := Privacy(s); p {
	case PrivacyPrivate, PrivacyUnlisted, PrivacyPublic:
		return p, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "privacy must be private, unlisted or public")
}

// Channel is a workspace's connected YouTube channel. The refresh token
// is stored sealed (pkg/secretbox); this type never sees it in clear.
type Channel struct {
	WorkspaceID uuid.UUID
	ChannelID   string
	Title       string
	SealedToken string
	Privacy     Privacy
	ConnectedBy uuid.UUID
	ConnectedAt time.Time
	UpdatedAt   time.Time
}

// NewChannel connects a channel. New connections upload as private: until
// the YouTube API audit passes, API uploads cannot be made public anyway.
func NewChannel(ws uuid.UUID, channelID, title, sealedToken string, by uuid.UUID, now time.Time) (Channel, error) {
	if ws == uuid.Nil || channelID == "" || sealedToken == "" {
		return Channel{}, apperror.New(apperror.ValidationFailed, "channel id and token are required")
	}
	now = timeutil.Normalize(now)
	return Channel{WorkspaceID: ws, ChannelID: channelID, Title: title, SealedToken: sealedToken, Privacy: PrivacyPrivate,
		ConnectedBy: by, ConnectedAt: now, UpdatedAt: now}, nil
}

// BlogKind is how articles reach the site.
type BlogKind string

const (
	// BlogGitHub commits to a GitHub repository (the site rebuilds on push).
	BlogGitHub BlogKind = "github"
	// BlogDir writes into a local checkout (for a site deployed from the
	// same machine, or for review before committing by hand).
	BlogDir BlogKind = "dir"
)

// BlogTarget is a workspace's blog.
type BlogTarget struct {
	WorkspaceID uuid.UUID
	Kind        BlogKind
	Repo        string // owner/name (github)
	Branch      string // github
	Dir         string // local checkout root (dir)
	ContentDir  string // e.g. marketing/src/content/blog; posts go to <ContentDir>/<locale>/<slug>.md
	PublicDir   string // e.g. marketing/public/blog; images go to <PublicDir>/<slug>/
	SiteURL     string // e.g. https://example.com
	// PathTemplate builds a post's URL path: {locale} is "" for the
	// default locale and "en/" otherwise, {slug} the slug.
	PathTemplate  string
	DefaultLocale string
	Author        string
	Draft         bool // publish with draft: true for a final human pass
	SealedToken   string
	UpdatedAt     time.Time
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Validate checks the target and fills defaults.
func (b *BlogTarget) Validate() error {
	fail := func(m string) error { return apperror.New(apperror.ValidationFailed, m) }
	switch b.Kind {
	case BlogGitHub:
		if !repoPattern.MatchString(b.Repo) {
			return fail("repository must be owner/name")
		}
		if b.Branch == "" {
			b.Branch = "main"
		}
		if b.SealedToken == "" {
			return fail("a GitHub token with contents write access is required")
		}
	case BlogDir:
		if b.Dir == "" || !path.IsAbs(b.Dir) {
			return fail("the checkout directory must be an absolute path")
		}
	default:
		return fail("kind must be github or dir")
	}
	for _, p := range []*string{&b.ContentDir, &b.PublicDir} {
		*p = strings.Trim(path.Clean("/"+*p), "/")
		if strings.Contains(*p, "..") {
			return fail("paths must stay inside the repository")
		}
	}
	if b.ContentDir == "" {
		return fail("content directory is required")
	}
	if b.PathTemplate == "" {
		b.PathTemplate = "/{locale}blog/{slug}/"
	}
	if !strings.Contains(b.PathTemplate, "{slug}") {
		return fail("the URL template must contain {slug}")
	}
	if b.DefaultLocale == "" {
		b.DefaultLocale = "tr"
	}
	if b.SiteURL != "" && !strings.HasPrefix(b.SiteURL, "https://") && !strings.HasPrefix(b.SiteURL, "http://") {
		return fail("site URL must start with https://")
	}
	b.SiteURL = strings.TrimRight(b.SiteURL, "/")
	return nil
}

// PostURL is where a post will live.
func (b BlogTarget) PostURL(locale, slug string) string {
	prefix := ""
	if locale != b.DefaultLocale {
		prefix = locale + "/"
	}
	return b.SiteURL + strings.NewReplacer("{locale}", prefix, "{slug}", slug).Replace(b.PathTemplate)
}
