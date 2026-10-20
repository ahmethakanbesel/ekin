package pexels_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/pexels"
)

func TestSearchVideoPicksSmallestAdequate(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/videos/search":
			if r.Header.Get("Authorization") != "key" || r.URL.Query().Get("orientation") != "portrait" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"videos":[{"id":1,"url":"https://pexels.com/v/1","duration":12,"user":{"name":"Ana","url":"u"},
			"video_files":[{"link":"` + srv.URL + `/f/4k","file_type":"video/mp4","width":2160,"height":3840},
			{"link":"` + srv.URL + `/f/sd","file_type":"video/mp4","width":360,"height":640},
			{"link":"` + srv.URL + `/f/hd","file_type":"video/mp4","width":1080,"height":1920}]}]}`))
		case "/f/hd":
			_, _ = w.Write([]byte("video-bytes"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := &pexels.Client{APIKey: "key", BaseURL: srv.URL}
	got, err := c.Search(context.Background(), "video", "calm sea", "portrait", 1, 1080)
	if err != nil || len(got) != 1 || got[0].Width != 1080 || got[0].Author != "Ana" || got[0].OriginalURL != srv.URL+"/f/4k" {
		t.Fatalf("search = %+v, %v", got, err)
	}
	p := filepath.Join(t.TempDir(), "a.mp4")
	if err := c.Download(context.Background(), got[0], p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "video-bytes" {
		t.Fatalf("downloaded %q", b)
	}
}

func TestDisabledWithoutKey(t *testing.T) {
	if _, err := (&pexels.Client{}).Search(context.Background(), "photo", "x", "", 1, 0); !errors.Is(err, pexels.ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
}
