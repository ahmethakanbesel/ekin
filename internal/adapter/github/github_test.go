package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/github"
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

// fakeRepo answers the Git Data and pulls endpoints for o/site.
type fakeRepo struct {
	calls     []string
	openPulls string // the JSON list GET /pulls answers
	branchSet bool   // refs/heads/ekin/x already exists
	created   map[string]any
	moved     map[string]any
	pulled    map[string]any
	pullQuery string
}

func (f *fakeRepo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	raw, _ := io.ReadAll(r.Body)
	p := strings.TrimPrefix(r.URL.Path, "/repos/o/site")
	switch {
	case r.Method == http.MethodGet && p == "/pulls":
		f.pullQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(cmpOr(f.openPulls, "[]")))
	case r.Method == http.MethodPost && p == "/pulls":
		_ = json.Unmarshal(raw, &f.pulled)
		_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/o/site/pull/7","state":"open"}`))
	case p == "/git/ref/heads/main":
		_, _ = w.Write([]byte(`{"object":{"sha":"head"}}`))
	case p == "/git/commits/head":
		_, _ = w.Write([]byte(`{"tree":{"sha":"basetree"}}`))
	case p == "/git/blobs":
		_, _ = w.Write([]byte(`{"sha":"blob"}`))
	case p == "/git/trees":
		_, _ = w.Write([]byte(`{"sha":"newtree"}`))
	case p == "/git/commits":
		_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
	case r.Method == http.MethodPost && p == "/git/refs":
		_ = json.Unmarshal(raw, &f.created)
		if f.branchSet {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Reference already exists"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	case r.Method == http.MethodPatch && p == "/git/refs/heads/ekin/x":
		_ = json.Unmarshal(raw, &f.moved)
		_, _ = w.Write([]byte(`{}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var prInput = github.PullRequestInput{
	Repo: "o/site", Base: "main", Branch: "ekin/x", Title: "blog: add post", Body: "From Ekin.",
	Files: []github.File{{Path: "content/blog/en/x.md", Content: []byte("---")}},
}

func TestOpenPullRequest(t *testing.T) {
	f := &fakeRepo{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	pr, err := (&github.Client{BaseURL: srv.URL}).OpenPullRequest(context.Background(), "tok", prInput)
	if err != nil {
		t.Fatal(err)
	}
	if pr != (github.PullRequest{Number: 7, URL: "https://github.com/o/site/pull/7", Branch: "ekin/x"}) {
		t.Fatalf("pr = %+v", pr)
	}
	if f.pullQuery != "base=main&head=o%3Aekin%2Fx&state=open" {
		t.Fatalf("pull lookup query = %q", f.pullQuery)
	}
	if f.created["ref"] != "refs/heads/ekin/x" || f.created["sha"] != "newcommit" {
		t.Fatalf("created ref = %v", f.created)
	}
	if f.pulled["head"] != "ekin/x" || f.pulled["base"] != "main" || f.pulled["title"] != "blog: add post" || f.pulled["body"] != "From Ekin." {
		t.Fatalf("pull request = %v", f.pulled)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "refs/heads/main") && strings.HasPrefix(c, "PATCH") {
			t.Fatalf("the base branch moved: %v", f.calls)
		}
	}
}

func TestOpenPullRequestResumes(t *testing.T) {
	t.Run("an open pull request is returned as it is", func(t *testing.T) {
		f := &fakeRepo{openPulls: `[{"number":3,"html_url":"https://github.com/o/site/pull/3","state":"open"}]`}
		srv := httptest.NewServer(f)
		defer srv.Close()
		pr, err := (&github.Client{BaseURL: srv.URL}).OpenPullRequest(context.Background(), "tok", prInput)
		if err != nil || pr.Number != 3 || pr.URL != "https://github.com/o/site/pull/3" {
			t.Fatalf("pr = %+v %v", pr, err)
		}
		if len(f.calls) != 1 {
			t.Fatalf("wrote after finding the pull request: %v", f.calls)
		}
	})
	t.Run("a branch left without a pull request moves to the new commit", func(t *testing.T) {
		f := &fakeRepo{branchSet: true}
		srv := httptest.NewServer(f)
		defer srv.Close()
		pr, err := (&github.Client{BaseURL: srv.URL}).OpenPullRequest(context.Background(), "tok", prInput)
		if err != nil || pr.Number != 7 {
			t.Fatalf("pr = %+v %v", pr, err)
		}
		if f.moved["sha"] != "newcommit" || f.moved["force"] != true {
			t.Fatalf("moved = %v", f.moved)
		}
	})
}

func TestOpenPullRequestRefusesBaseAsHead(t *testing.T) {
	in := prInput
	in.Branch = "main"
	if _, err := (&github.Client{BaseURL: "http://127.0.0.1:1"}).OpenPullRequest(context.Background(), "tok", in); err == nil {
		t.Fatal("opened a pull request from the base branch onto itself")
	}
}

func TestPullRequestState(t *testing.T) {
	for _, tc := range []struct {
		body string
		want github.PullState
	}{
		{`{"state":"open","merged":false}`, github.PullOpen},
		{`{"state":"closed","merged":true}`, github.PullMerged},
		{`{"state":"closed","merged":false}`, github.PullClosed},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/o/site/pulls/7" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(tc.body))
		}))
		got, err := (&github.Client{BaseURL: srv.URL}).PullRequestState(context.Background(), "tok", "o/site", 7)
		srv.Close()
		if err != nil || got != tc.want {
			t.Fatalf("%s: state = %q %v, want %q", tc.body, got, err, tc.want)
		}
	}
}

func TestErrorCarriesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()
	_, err := (&github.Client{BaseURL: srv.URL}).PullRequestState(context.Background(), "tok", "o/site", 9)
	var apiErr *github.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound || apiErr.Message != "Not Found" {
		t.Fatalf("err = %v", err)
	}
}
