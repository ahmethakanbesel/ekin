package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kurgu/internal/adapter/github"
)

func TestCommit(t *testing.T) {
	var calls []string
	var tree map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/git/ref/heads/main"):
			_, _ = w.Write([]byte(`{"object":{"sha":"head"}}`))
		case strings.HasSuffix(r.URL.Path, "/git/commits/head"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"basetree"}}`))
		case strings.HasSuffix(r.URL.Path, "/git/blobs"):
			_, _ = w.Write([]byte(`{"sha":"blob"}`))
		case strings.HasSuffix(r.URL.Path, "/git/trees"):
			_ = json.Unmarshal(raw, &tree)
			_, _ = w.Write([]byte(`{"sha":"newtree"}`))
		case strings.HasSuffix(r.URL.Path, "/git/commits"):
			_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
		case strings.HasSuffix(r.URL.Path, "/git/refs/heads/main"):
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	sha, err := (&github.Client{BaseURL: srv.URL}).Commit(context.Background(), "tok", "o/site", "main", "blog: add post",
		[]github.File{{Path: "/marketing/src/content/blog/tr/x.md", Content: []byte("---")}, {Path: "marketing/public/blog/x/hero.jpg", Content: []byte{1}}})
	if err != nil || sha != "newcommit" {
		t.Fatalf("commit = %q %v", sha, err)
	}
	if tree["base_tree"] != "basetree" || len(tree["tree"].([]any)) != 2 {
		t.Fatalf("tree = %v", tree)
	}
	if calls[len(calls)-1] != "PATCH /repos/o/site/git/refs/heads/main" {
		t.Fatalf("calls = %v", calls)
	}
}
