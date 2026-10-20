package elevenlabs_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/elevenlabs"
)

func TestClient(t *testing.T) {
	limited := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("xi-api-key") != "sk_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"detail":{"status":"invalid_api_key","message":"Invalid API key"}}`))
			return
		}
		switch r.URL.Path {
		case "/v2/voices":
			if r.URL.Query().Get("next_page_token") == "" {
				_, _ = w.Write([]byte(`{"voices":[{"voice_id":"a1","name":"Ada","category":"premade","labels":{"gender":"female"},
					"verified_languages":[{"language":"tr","model_id":"eleven_v3"},{"language":"tr","model_id":"eleven_multilingual_v2"}]}],
					"has_more":true,"next_page_token":"p2"}`))
				return
			}
			_, _ = w.Write([]byte(`{"voices":[{"voice_id":"b2","name":"Bo","category":"cloned"}],"has_more":false}`))
		case "/v1/models":
			_, _ = w.Write([]byte(`[{"model_id":"eleven_multilingual_v2","name":"Multilingual v2","can_do_text_to_speech":true,
				"maximum_text_length_per_request":10000,"languages":[{"language_id":"tr","name":"Turkish"}],
				"model_rates":{"character_cost_multiplier":1}},
				{"model_id":"eleven_multilingual_sts_v2","can_do_text_to_speech":false}]`))
		case "/v1/user/subscription":
			if limited++; limited == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"detail":{"status":"too_many_concurrent_requests","message":"slow down"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"tier":"creator","status":"active","character_count":1200,"character_limit":100000}`))
		default:
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"detail":[{"loc":["body","text"],"msg":"field required","type":"missing"}]}`))
		}
	}))
	defer srv.Close()
	c := &elevenlabs.Client{APIKey: "sk_test", BaseURL: srv.URL, Retry: 1}
	ctx := context.Background()

	voices, err := c.Voices(ctx, 50)
	if err != nil || len(voices) != 2 || voices[0].ID != "a1" || len(voices[0].Languages) != 1 || voices[1].Name != "Bo" {
		t.Fatalf("voices = %+v, %v", voices, err)
	}
	models, err := c.Models(ctx)
	if err != nil || len(models) != 1 || models[0].MaxCharacters != 10000 || models[0].Languages[0] != "tr" {
		t.Fatalf("models = %+v, %v", models, err)
	}
	sub, err := c.Subscription(ctx) // the first try is rate-limited and retried
	if err != nil || sub.CharacterLimit-sub.CharacterCount != 98800 || limited != 2 {
		t.Fatalf("subscription = %+v, %v after %d tries", sub, err, limited)
	}

	var apiErr *elevenlabs.Error
	if _, err := c.Speak(ctx, "a1", elevenlabs.SpeakRequest{}); !errors.As(err, &apiErr) || apiErr.Message != "field required" {
		t.Fatalf("validation error = %v", err)
	}
	bad := &elevenlabs.Client{APIKey: "sk_bad", BaseURL: srv.URL}
	if _, err := bad.Models(ctx); !errors.As(err, &apiErr) || apiErr.Code != "invalid_api_key" || apiErr.Status != 401 {
		t.Fatalf("bad key = %v", err)
	}
	if _, err := (&elevenlabs.Client{}).Models(ctx); !errors.Is(err, elevenlabs.ErrNoKey) {
		t.Fatalf("no key = %v", err)
	}
}
