package app_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/app"
	"github.com/ahmethakanbesel/ekin/internal/config"
	identitydomain "github.com/ahmethakanbesel/ekin/internal/domain/identity/v1"
)

// Every API operation is wired to a service: a missing dependency in the
// composition root panics on the first request, so call one GET per
// area through the real router.
func TestRouterIsFullyWired(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DataDir, cfg.DBPath = dir, dir+"/kurgu.db"
	cfg.Blob.Dir, cfg.Production.WorkDir, cfg.Publishing.SecretKeyFile = dir+"/blobs", dir+"/work", dir+"/secret.key"
	a, err := app.New(ctx, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	email, _ := identitydomain.ParseEmail("a@example.com")
	pw, _ := identitydomain.ParsePassword("correct-horse-1")
	if _, err := a.Identity().CreateUser(ctx, email, "Ada", pw); err != nil {
		t.Fatal(err)
	}
	router, err := a.Router()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	post := func(path, body string) *http.Response {
		res, err := c.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	_ = post("/api/v1/auth/login", `{"email":"a@example.com","password":"correct-horse-1"}`).Body.Close()
	res := post("/api/v1/workspaces", `{"name":"ws","timezone":"UTC"}`)
	var ws struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&ws)
	_ = res.Body.Close()
	base := "/api/v1/workspaces/" + ws.ID
	for _, path := range []string{
		"/api/v1/me", "/api/v1/system", "/api/v1/system/profiles", "/api/v1/system/credentials",
		base, base + "/members", base + "/invitations", base + "/pipelines", base + "/jobs", base + "/documents",
		base + "/stories", base + "/notifications", base + "/integrations", base + "/seo", base + "/opportunities",
		base + "/templates", base + "/voice", "/api/v1/voices", base + "/seo/openseo",
	} {
		res, err := c.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d", path, res.StatusCode)
		}
	}
}
