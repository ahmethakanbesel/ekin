package openseo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/adapter/openseo"
)

// fakeMCP answers like OpenSEO's stateless endpoint.
func fakeMCP(t *testing.T, sse bool, tools map[string]func(args map[string]any) (any, bool)) (*httptest.Server, *[]string) {
	t.Helper()
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer oseo_good" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_api_key","error_description":"The provided API key is invalid, expired, or disabled"}`))
			return
		}
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		methods = append(methods, req.Method+":"+req.Params.Name)
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}}
		if req.Method == "tools/call" {
			fn, ok := tools[req.Params.Name]
			if !ok {
				t.Fatalf("unexpected tool %s", req.Params.Name)
			}
			structured, isErr := fn(req.Params.Arguments)
			if isErr {
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": structured}}, "isError": true}
			} else {
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}, "structuredContent": structured}
			}
		}
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", raw)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv, &methods
}

func TestClient(t *testing.T) {
	ctx := context.Background()
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%v", sse), func(t *testing.T) {
			var gotArgs map[string]any
			srv, methods := fakeMCP(t, sse, map[string]func(map[string]any) (any, bool){
				"whoami": func(map[string]any) (any, bool) {
					return map[string]any{"userEmail": "a@example.com", "mode": "hosted", "scopes": []string{}, "creditsRemaining": 1250}, false
				},
				"research_keywords": func(a map[string]any) (any, bool) {
					gotArgs = a
					return map[string]any{"results": []any{
						map[string]any{"seed": "kaygı", "ok": true, "rows": []any{
							map[string]any{"keyword": "kaygı bozukluğu", "searchVolume": 8100, "keywordDifficulty": 31, "intent": "informational"},
							map[string]any{"keyword": "kaygı testi", "searchVolume": nil, "keywordDifficulty": nil, "intent": "navigational"},
						}},
						map[string]any{"seed": "x", "ok": false, "error": "no data"},
					}}, false
				},
				"list_projects": func(map[string]any) (any, bool) { return "project access denied", true },
			})
			c := &openseo.Client{URL: srv.URL, APIKey: "oseo_good"}
			a, err := c.Whoami(ctx)
			if err != nil || a.UserEmail != "a@example.com" || a.CreditsRemaining == nil || *a.CreditsRemaining != 1250 {
				t.Fatalf("whoami = %+v %v", a, err)
			}
			res, err := c.ResearchKeywords(ctx, "p1", []string{"kaygı", "x"}, openseo.Market{LocationCode: 2792, LanguageCode: "tr"})
			if err != nil || len(res) != 2 || !res[0].OK || res[1].OK || len(res[0].Rows) != 2 {
				t.Fatalf("research = %+v %v", res, err)
			}
			if r := res[0].Rows[0]; *r.SearchVolume != 8100 || *r.KeywordDifficulty != 31 || res[0].Rows[1].SearchVolume != nil {
				t.Fatalf("rows = %+v", res[0].Rows)
			}
			seeds := gotArgs["seeds"].([]any)
			if gotArgs["projectId"] != "p1" || seeds[0].(map[string]any)["locationCode"] != float64(2792) {
				t.Fatalf("args = %v", gotArgs)
			}
			var te *openseo.Error
			if _, err := c.Projects(ctx); !errors.As(err, &te) || te.Status != 0 {
				t.Fatalf("tool error = %v", err)
			}
			if (*methods)[0] != "initialize:" || (*methods)[1] != "notifications/initialized:" || len(*methods) != 5 {
				t.Fatalf("calls = %v (initialize once)", *methods)
			}
		})
	}
}

func TestClientHTTPErrors(t *testing.T) {
	ctx := context.Background()
	srv, _ := fakeMCP(t, false, nil)
	_, err := (&openseo.Client{URL: srv.URL, APIKey: "oseo_bad"}).Whoami(ctx)
	var e *openseo.Error
	if !errors.As(err, &e) || e.Status != 401 || e.Code != "invalid_api_key" {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := (&openseo.Client{URL: srv.URL}).Whoami(ctx); !errors.As(err, &e) || e.Code != "no_api_key" {
		t.Fatalf("no key: %v", err)
	}
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"usage_exceeded","error_description":"Out of credits"}`))
	}))
	defer limited.Close()
	_, err = (&openseo.Client{URL: limited.URL, APIKey: "oseo_x"}).Whoami(ctx)
	if !errors.As(err, &e) || e.Status != 429 || e.Code != "usage_exceeded" || e.RetryAfter != 30*time.Second {
		t.Fatalf("limited: %v", err)
	}
}
