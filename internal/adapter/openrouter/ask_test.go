package openrouter_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/openrouter"
)

func TestAsk(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sent)
		if r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(401)
			return
		}
		if strings.Contains(string(raw), "broken/model") {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"not a valid model"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Try Acme (https://acme.com/pricing). Or Globex, see https://globex.io.",
			"annotations":[{"type":"url_citation","url_citation":{"url":"https://review.example/best-tools"}},{"type":"url_citation","url_citation":{"url":"https://acme.com/pricing"}}]}}],
			"usage":{"cost":0.0042}}`))
	}))
	defer srv.Close()
	c := &openrouter.Client{APIKey: "key", BaseURL: srv.URL}
	a, err := c.Ask(context.Background(), "perplexity/sonar:online", "What are the best tools for small teams?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.Text, "Try Acme") || a.Cost != 0.0042 {
		t.Fatalf("answer = %+v", a)
	}
	want := []string{"https://review.example/best-tools", "https://acme.com/pricing", "https://globex.io"}
	if strings.Join(a.Citations, " ") != strings.Join(want, " ") {
		t.Fatalf("citations = %v", a.Citations)
	}
	msgs := sent["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["role"] != "user" || sent["model"] != "perplexity/sonar:online" {
		t.Fatalf("sent = %v", sent)
	}
	if _, err := c.Ask(context.Background(), "broken/model", "x"); err == nil || !strings.Contains(err.Error(), "not a valid model") {
		t.Fatalf("bad model = %v", err)
	}
	if _, err := (&openrouter.Client{}).Ask(context.Background(), "m/x", "x"); err == nil {
		t.Fatal("asked without a key")
	}
}
