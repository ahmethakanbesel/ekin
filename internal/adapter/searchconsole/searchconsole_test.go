package searchconsole_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/adapter/searchconsole"
)

func TestFlow(t *testing.T) {
	var query map[string]any
	var path string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "authorization_code" {
			_, _ = w.Write([]byte(`{"access_token":"a1","refresh_token":"r1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"a2"}`))
	})
	mux.HandleFunc("GET /api/sites", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a1" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"siteEntry":[{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"},{"siteUrl":"https://other.com/","permissionLevel":"siteUnverifiedUser"}]}`))
	})
	mux.HandleFunc("POST /api/sites/", func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &query)
		_, _ = w.Write([]byte(`{"rows":[{"keys":["2026-10-01","https://example.com/a"],"clicks":3,"impressions":40,"ctr":0.075,"position":7.5}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &searchconsole.Client{ClientID: "id", ClientSecret: "s", RedirectURL: "http://x/cb",
		Endpoints: searchconsole.Endpoints{Auth: srv.URL + "/auth", Token: srv.URL + "/token", API: srv.URL + "/api"}}
	u, _ := url.Parse(c.AuthURL("st"))
	if u.Query().Get("scope") != searchconsole.Scope || u.Query().Get("access_type") != "offline" || u.Query().Get("state") != "st" {
		t.Fatalf("auth url = %s", u)
	}
	ctx := context.Background()
	refresh, access, err := c.Exchange(ctx, "code")
	if err != nil || refresh != "r1" || access != "a1" {
		t.Fatalf("exchange = %q %q %v", refresh, access, err)
	}
	props, err := c.Properties(ctx, access)
	if err != nil || len(props) != 1 || props[0].URL != "sc-domain:example.com" {
		t.Fatalf("properties = %+v %v", props, err)
	}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rows, err := c.Analytics(ctx, access, "sc-domain:example.com", searchconsole.Query{Start: day, End: day, Dimensions: []string{"date", "page"}, RowLimit: 100})
	if err != nil || len(rows) != 1 || rows[0].Clicks != 3 || rows[0].Keys[1] != "https://example.com/a" {
		t.Fatalf("rows = %+v %v", rows, err)
	}
	if !strings.Contains(path, "sc-domain:example.com") || query["startDate"] != "2026-10-01" || query["type"] != "web" {
		t.Fatalf("query %s %v", path, query)
	}
	if a, err := c.Access(ctx, refresh); err != nil || a != "a2" {
		t.Fatalf("access = %q %v", a, err)
	}
	var apiErr *searchconsole.Error
	if _, err := c.Properties(ctx, "bad"); !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Fatalf("unauthorised = %v", err)
	}
}
