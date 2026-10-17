package openrouter_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ekin/internal/adapter/openrouter"
	storydomain "ekin/internal/domain/story/v1"
	storyrepo "ekin/internal/repository/story/v1"
)

func TestDedupe(t *testing.T) {
	var seen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &seen)
		if r.Header.Get("Authorization") != "Bearer k" || r.URL.Path != "/chat/completions" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"duplicate\":true,\"reason\":\"same takeaway\"}"}}]}`))
	}))
	defer srv.Close()
	d := openrouter.Dedupe{Client: &openrouter.Client{APIKey: "k", BaseURL: srv.URL}}
	idea, _ := storydomain.NewIdea("Kaygı ve endişe farkı", "beden üzerinden", nil)
	dup, why, err := d.Duplicate(context.Background(), idea, []storyrepo.Match{{Title: "Kaygı ile endişe arasındaki fark"}})
	if err != nil || !dup || why != "same takeaway" {
		t.Fatalf("verdict = %v %q %v", dup, why, err)
	}
	if seen["model"] != "typesafe/jev-1.13" || !strings.Contains(seen["messages"].([]any)[1].(map[string]any)["content"].(string), "Kaygı ile endişe") {
		t.Fatalf("request = %v", seen)
	}
	off := openrouter.Dedupe{Client: &openrouter.Client{}}
	if dup, _, err := off.Duplicate(context.Background(), idea, nil); dup || err != nil {
		t.Fatal("disabled client decided")
	}
}
