package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/adapter/claude"
	"github.com/ahmethakanbesel/ekin/internal/adapter/extract"
	"github.com/ahmethakanbesel/ekin/internal/adapter/remote"
	"github.com/ahmethakanbesel/ekin/internal/adapter/render"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/blob"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	jobrepo "github.com/ahmethakanbesel/ekin/internal/repository/job/v1"
	knowledgerepo "github.com/ahmethakanbesel/ekin/internal/repository/knowledge/v1"
	pipelinerepo "github.com/ahmethakanbesel/ekin/internal/repository/pipeline/v1"
	storyrepo "github.com/ahmethakanbesel/ekin/internal/repository/story/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	knowledgesvc "github.com/ahmethakanbesel/ekin/internal/service/knowledge/v1"
	pipelinesvc "github.com/ahmethakanbesel/ekin/internal/service/pipeline/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	workerapi "github.com/ahmethakanbesel/ekin/internal/transport/http/worker"
	remoteworker "github.com/ahmethakanbesel/ekin/internal/transport/job/remote/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// agent answers idea prompts and writes a valid video into the dir.
type agent struct{ host string }

func (a agent) Run(_ context.Context, req claude.Request) (claude.Result, error) {
	if len(req.JSONSchema) > 0 {
		return claude.Result{Structured: []byte(`{"ideas":[{"title":"Ruminasyon nedir","angle":"döngü","keywords":["ruminasyon"]}]}`)}, nil
	}
	if _, err := os.Stat(filepath.Join(req.Dir, "BRIEF.md")); err != nil {
		return claude.Result{}, errors.New("the work dir did not arrive")
	}
	files := map[string]string{
		"script.md": "s", "narration/speech.wav": "RIFF", "composition.html": `<script src="/kit/ekin.js"></script>`,
		"narration/timings.json": `{"duration": 20, "words": [{"text":"a","start":0,"end":1}]}`,
		"metadata.json":          `{"title":"Ruminasyon","description":"d","tags":[]}`,
		"made-on":                a.host,
	}
	for name, content := range files {
		p := filepath.Join(req.Dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		_ = os.WriteFile(p, []byte(content), 0o600)
	}
	return claude.Result{SessionID: "s-" + a.host, Text: "done on " + a.host}, nil
}

type renderer struct{}

func (renderer) Probe(context.Context, string, string) (render.Meta, []string, error) {
	return render.Meta{Width: 1080, Height: 1920, FPS: 30, Duration: 21}, nil, nil
}
func (renderer) Video(_ context.Context, _, _ string, _ render.VideoOptions, out string) (render.Meta, error) {
	return render.Meta{Duration: 21}, os.WriteFile(out, []byte("v"), 0o600)
}
func (renderer) Music(context.Context, string, float64, string) (float64, error) { return 0, nil }
func (renderer) MusicTrack(_ context.Context, _ string, _, _ float64, out string) error {
	return os.WriteFile(out, []byte("track"), 0o600)
}
func (renderer) Mix(_ context.Context, _, _, _ string, _ float64, out string) error {
	return os.WriteFile(out, []byte("remote-render"), 0o600)
}
func (renderer) Poster(_ context.Context, _ string, _ float64, out string) error {
	return os.WriteFile(out, []byte("jpg"), 0o600)
}
func (renderer) Thumbnails(_ context.Context, _, _ string, ns []int, _ bool, outDir string) ([]render.ThumbnailShot, render.Meta, []string, error) {
	_ = os.MkdirAll(outDir, 0o750)
	var shots []render.ThumbnailShot
	for _, n := range ns {
		p := filepath.Join(outDir, strconv.Itoa(n)+".jpg")
		_ = os.WriteFile(p, []byte("thumb"), 0o600)
		shots = append(shots, render.ThumbnailShot{N: n, Path: p})
	}
	return shots, render.Meta{Width: 1280, Height: 720, FPS: 1, Duration: 1}, nil, nil
}

func TestRemoteWorkerDraftsAndRenders(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
	workspaces := workspacerepo.NewSQLite(tx)
	_ = workspaces.Create(ctx, w)
	blobs, _ := blob.NewFS(t.TempDir())
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	knowledge := knowledgesvc.NewService(knowledgerepo.NewSQLite(tx), blobs, jobs, extract.New(), tx)
	pipelines := pipelinesvc.NewService(pipelinerepo.NewSQLite(tx), workspaces, jobs, tx)
	server := productionsvc.NewService(productionsvc.Config{WorkRoot: t.TempDir()}, productionsvc.Deps{
		Stories: storyrepo.NewSQLite(tx), Pipelines: pipelines, Knowledge: knowledge, Jobs: jobs, Tx: tx, Blobs: blobs,
		Agent: agent{host: "server"}, Renderer: renderer{}, Log: log,
	})

	const token = "a-worker-token-of-enough-length"
	mux := http.NewServeMux()
	workerapi.Register(mux, token, jobs, server, nil, log, func(w http.ResponseWriter, _ *http.Request, err error) {
		code, _ := apperror.GetCode(err)
		w.WriteHeader(apperror.StatusFor(code))
		_ = json.NewEncoder(w).Encode(map[string]string{"code": string(code), "message": err.Error()})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	if _, err := server.QueueAdHoc(ctx, w.ID(), id.New(), productionsvc.AdHocRequest{Idea: "x", Count: 1, Language: pipelinedomain.Turkish, Outputs: outputs}); err != nil {
		t.Fatal(err)
	}
	// Planning stays on the server.
	j, _ := jobs.Claim(ctx, []jobdomain.Queue{jobdomain.QueueControl}, []string{productionsvc.KindAdHoc}, "server", time.Minute)
	var plan productionsvc.AdHocPayload
	_ = json.Unmarshal(j.Payload(), &plan)
	if err := server.PlanAdHoc(ctx, plan); err != nil {
		t.Fatal(err)
	}
	_ = jobs.Complete(ctx, j.ID(), "server", nil)

	bad := &remote.Client{BaseURL: srv.URL, Token: "wrong-token-wrong-token-wrong", Owner: "mac"}
	if _, err := bad.Claim(ctx, jobdomain.Queues, []string{productionsvc.KindDraft}, "", time.Minute); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("bad token claim = %v", err)
	}

	client := &remote.Client{BaseURL: srv.URL, Token: token, Owner: "mac"}
	mac := productionsvc.NewService(productionsvc.Config{WorkRoot: t.TempDir()}, productionsvc.Deps{Agent: agent{host: "mac"}, Renderer: renderer{}, Log: log})
	runner := jobsvc.NewRunner(client, jobsvc.RunnerConfig{
		Owner: "mac", Poll: 10 * time.Millisecond, Lease: time.Minute,
		Throttle: jobsvc.Throttle{Concurrency: map[jobdomain.Queue]int{jobdomain.QueueAgent: 1, jobdomain.QueueRender: 1}},
	}, log)
	remoteworker.Register(runner, client, mac)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { _ = runner.Run(runCtx); close(done) }()
	defer func() { cancel(); <-done }()

	stories := storyrepo.NewSQLite(tx)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		list, _, _ := stories.List(ctx, w.ID(), storyrepo.Filter{})
		if len(list) == 1 && list[0].Lead().Status() == storydomain.StatusReview {
			lead := list[0].Lead()
			if lead.Session() != "s-mac" {
				t.Fatalf("session = %q", lead.Session())
			}
			made, err := os.ReadFile(filepath.Join(server.WorkDir(lead.ID()), "made-on"))
			if err != nil || string(made) != "mac" {
				t.Fatalf("server work dir not replaced by the remote result: %q %v", made, err)
			}
			rev, _ := stories.Revision(ctx, lead.ID(), 1)
			rc, _, err := blobs.Open(ctx, rev.Artifacts[storydomain.ArtifactPreview])
			if err != nil {
				t.Fatal(err)
			}
			b := make([]byte, 64)
			n, _ := rc.Read(b)
			_ = rc.Close()
			if string(b[:n]) != "remote-render" {
				t.Fatalf("preview = %q", b[:n])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	list, _, _ := stories.List(ctx, w.ID(), storyrepo.Filter{})
	t.Fatalf("story never reached review: %s %s", list[0].Lead().Status(), list[0].Lead().Failure())
}
