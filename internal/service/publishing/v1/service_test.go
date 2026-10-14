package publishing_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/github"
	"kurgu/internal/adapter/youtube"
	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	publishingdomain "kurgu/internal/domain/publishing/v1"
	storydomain "kurgu/internal/domain/story/v1"
	publishingrepo "kurgu/internal/repository/publishing/v1"
	productionsvc "kurgu/internal/service/production/v1"
	publishingsvc "kurgu/internal/service/publishing/v1"
	"kurgu/pkg/id"
	"kurgu/pkg/secretbox"
)

func fakeGoogle(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	privacy := new(string)
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r","expires_in":3600}`))
	})
	mux.HandleFunc("GET /api/channels", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":"UC1","snippet":{"title":"Psikoloji"}}]}`))
	})
	mux.HandleFunc("POST /upload/videos", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), `"privacyStatus":"unlisted"`) {
			*privacy = "unlisted"
		}
		w.Header().Set("Location", srv.URL+"/session")
	})
	mux.HandleFunc("PUT /session", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"id":"vid9"}`)) })
	mux.HandleFunc("POST /upload/thumbnails/set", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, privacy
}

func newService(t *testing.T, google *httptest.Server) *publishingsvc.Service {
	t.Helper()
	box, _ := secretbox.New([]byte("test-secret-0123456789"))
	yt := &youtube.Client{ClientID: "id", ClientSecret: "s", RedirectURL: "https://k/cb"}
	if google != nil {
		yt.Endpoints = youtube.Endpoints{Auth: google.URL + "/auth", Token: google.URL + "/token", API: google.URL + "/api", Upload: google.URL + "/upload/videos"}
	}
	return publishingsvc.NewService(publishingrepo.NewInMemory(), box, func(context.Context) *youtube.Client { return yt }, &github.Client{}, slog.New(slog.DiscardHandler))
}

func request(t *testing.T, kind pipelinedomain.Kind, files map[string]string, lead *storydomain.Publication) productionsvc.PublishRequest {
	t.Helper()
	v, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 30*time.Second)
	a, _ := pipelinedomain.NewArticleSpec(900, nil)
	var o pipelinedomain.Outputs
	if kind == pipelinedomain.KindVideo {
		o, _ = pipelinedomain.NewOutputs(&v, nil, "")
	} else {
		o, _ = pipelinedomain.NewOutputs(nil, &a, "")
	}
	idea, _ := storydomain.NewIdea("Ruminasyon nedir", "x", nil)
	st, _ := storydomain.New(id.New(), storydomain.Plan{WorkspaceID: id.New(), Origin: storydomain.OriginManual,
		Language: pipelinedomain.Turkish, Idea: idea, Outputs: o}, id.New, time.Now())
	return productionsvc.PublishRequest{
		Story: st, Deliverable: st.Lead(), Lead: lead, TranslationKey: "abc12345",
		Open: func(_ context.Context, name string) (io.ReadCloser, int64, error) {
			c, ok := files[name]
			if !ok {
				return nil, 0, apperror.New(apperror.NotFound, name)
			}
			return io.NopCloser(strings.NewReader(c)), int64(len(c)), nil
		},
	}
}

func TestConnectAndUpload(t *testing.T) {
	ctx := context.Background()
	google, privacy := fakeGoogle(t)
	svc := newService(t, google)
	ws, user := id.New(), id.New()

	link, err := svc.ConnectURL(ctx, ws, user)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	state := u.Query().Get("state")
	if _, err := svc.Callback(ctx, state+"x", "code"); err == nil {
		t.Fatal("tampered state accepted")
	}
	got, err := svc.Callback(ctx, state, "code")
	if err != nil || got != ws {
		t.Fatalf("callback = %v %v", got, err)
	}
	ch, ok, _ := svc.Channel(ctx, ws)
	if !ok || ch.Title != "Psikoloji" || ch.Privacy != publishingdomain.PrivacyPrivate || ch.SealedToken == "r" {
		t.Fatalf("channel = %+v", ch)
	}
	if _, err := svc.SetPrivacy(ctx, ws, publishingdomain.PrivacyUnlisted); err != nil {
		t.Fatal(err)
	}
	if ready, _ := svc.Video().Ready(ctx, ws); !ready {
		t.Fatal("video publisher not ready")
	}
	req := request(t, pipelinedomain.KindVideo, map[string]string{
		storydomain.ArtifactMetadata: `{"title":"Ruminasyon nedir? #shorts","description":"d","tags":["psikoloji"]}`,
		storydomain.ArtifactFinal:    "mp4", storydomain.ArtifactPoster: "jpg",
	}, nil)
	req.WorkspaceID = ws
	pub, err := svc.Video().Publish(ctx, req)
	if err != nil || pub.URL != "https://www.youtube.com/shorts/vid9" || *privacy != "unlisted" {
		t.Fatalf("publish = %+v %v privacy=%s", pub, err, *privacy)
	}
	// Google's error redirect (e.g. access_denied) carries the state, so
	// the browser can go back to the right workspace.
	if got := svc.StateWorkspace(state); got != ws {
		t.Fatalf("state workspace = %v, want %v", got, ws)
	}
	if got := svc.StateWorkspace(state + "x"); got != uuid.Nil {
		t.Fatalf("forged state workspace = %v", got)
	}
	expired := newService(t, google).WithClock(func() time.Time { return time.Now().Add(time.Hour) })
	if _, err := expired.Callback(ctx, state, "code"); err == nil {
		t.Fatal("state from another key accepted")
	}
}

func TestArticleToLocalCheckout(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)
	ws := id.New()
	dir := t.TempDir()
	if _, err := svc.SaveBlog(ctx, ws, publishingsvc.BlogInput{BlogTarget: publishingdomain.BlogTarget{
		Kind: publishingdomain.BlogDir, Dir: dir, ContentDir: "marketing/src/content/blog", SiteURL: "https://example.com", Author: "acme ekibi",
	}}); err != nil {
		t.Fatal(err)
	}
	body := "Giriş paragrafı.\n\n<YouTube id=\"vid9abcdef\" />\n\n## Bölüm\n\n" + strings.Repeat("kelime ", 200)
	article := "---\ntitle: \"Ruminasyon nedir\"\ndescription: \"" + strings.Repeat("açıklama ", 12) + "\"\nslug: ruminasyon-nedir\ntags: [ruminasyon]\n---\n\n" + body
	req := request(t, pipelinedomain.KindArticle, map[string]string{storydomain.ArtifactArticle: article},
		&storydomain.Publication{URL: "https://www.youtube.com/shorts/vid9abcdef", ExternalID: "vid9abcdef"})
	req.WorkspaceID = ws
	pub, err := svc.Article().Publish(ctx, req)
	if err != nil || pub.URL != "https://example.com/blog/ruminasyon-nedir/" {
		t.Fatalf("publish = %+v %v", pub, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "marketing/src/content/blog/tr/ruminasyon-nedir.md"))
	if err != nil {
		t.Fatal(err)
	}
	post := string(raw)
	for _, want := range []string{"locale: tr", "author: acme ekibi", "translationKey: abc12345", "youtube-nocookie.com/embed/vid9abcdef", "177.78%", "## Bölüm"} {
		if !strings.Contains(post, want) {
			t.Errorf("post lacks %q:\n%s", want, post[:400])
		}
	}
	if strings.Contains(post, "<YouTube") || strings.Contains(post, "slug:") {
		t.Error("post kept the agent-only marker or slug field")
	}
	if _, err := svc.SaveBlog(ctx, ws, publishingsvc.BlogInput{BlogTarget: publishingdomain.BlogTarget{Kind: publishingdomain.BlogGitHub, Repo: "bad", ContentDir: "x"}}); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("bad repo = %v", err)
	}
}
