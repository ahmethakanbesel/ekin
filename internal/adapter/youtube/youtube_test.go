package youtube_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/youtube"
)

func TestOAuthAndUpload(t *testing.T) {
	var uploaded []byte
	var meta map[string]any
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			_, _ = w.Write([]byte(`{"access_token":"a1","refresh_token":"r1","expires_in":3600}`))
		case "refresh_token":
			if r.Form.Get("refresh_token") != "r1" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"a2","expires_in":3600}`))
		}
	})
	mux.HandleFunc("GET /api/channels", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":"UC1","snippet":{"title":"Psikoloji"}}]}`))
	})
	mux.HandleFunc("POST /upload/videos", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a2" || r.URL.Query().Get("uploadType") != "resumable" {
			w.WriteHeader(401)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &meta)
		w.Header().Set("Location", srv.URL+"/session/1")
	})
	mux.HandleFunc("PUT /session/1", func(w http.ResponseWriter, r *http.Request) {
		uploaded, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"vid123"}`))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := &youtube.Client{ClientID: "id", ClientSecret: "s", RedirectURL: "https://k/cb",
		Endpoints: youtube.Endpoints{Auth: srv.URL + "/auth", Token: srv.URL + "/token", API: srv.URL + "/api", Upload: srv.URL + "/upload/videos"}}
	u, _ := url.Parse(c.AuthURL("st"))
	if u.Query().Get("access_type") != "offline" || u.Query().Get("state") != "st" || !strings.Contains(u.Query().Get("scope"), "youtube.upload") {
		t.Fatalf("auth url = %s", u)
	}
	refresh, access, err := c.Exchange(context.Background(), "code")
	if err != nil || refresh != "r1" || access != "a1" {
		t.Fatalf("exchange = %q %q %v", refresh, access, err)
	}
	id, title, err := c.Channel(context.Background(), access)
	if err != nil || id != "UC1" || title != "Psikoloji" {
		t.Fatalf("channel = %q %q %v", id, title, err)
	}
	access, err = c.Access(context.Background(), refresh)
	if err != nil {
		t.Fatal(err)
	}
	vid, err := c.Upload(context.Background(), access, youtube.Video{
		Title: "Ruminasyon nedir?", Description: "d", Tags: []string{"psikoloji"}, Language: "tr", Privacy: "private",
		Body: strings.NewReader("mp4-bytes"), Size: 9,
	})
	if err != nil || vid != "vid123" || string(uploaded) != "mp4-bytes" {
		t.Fatalf("upload = %q %v", vid, err)
	}
	status := meta["status"].(map[string]any)
	if status["privacyStatus"] != "private" || status["containsSyntheticMedia"] != true || meta["snippet"].(map[string]any)["categoryId"] != "27" {
		t.Fatalf("metadata = %v", meta)
	}
	if _, err := c.Access(context.Background(), "revoked"); err == nil {
		t.Fatal("revoked token accepted")
	}
}
