package httptransport_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"kurgu/internal/db/dbtest"
	"kurgu/internal/gen"
	jobrepo "kurgu/internal/repository/job/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	jobsvc "kurgu/internal/service/job/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	httptransport "kurgu/internal/transport/http"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	_, tx := dbtest.Open(t)
	workspaces := workspacerepo.NewSQLite(tx)
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	srv := httptest.NewServer(httptransport.NewRouter(httptransport.Deps{
		Workspaces: workspacesvc.NewService(workspaces),
		Pipelines:  pipelinesvc.NewService(pipelinerepo.NewSQLite(tx), workspaces, jobs, tx),
		Jobs:       jobs,
		UI:         fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Kurgu</title>")}},
		Log:        slog.New(slog.DiscardHandler),
	}))
	t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, srv *httptest.Server, method, path string, body any, wantStatus int, out any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		t.Fatalf("%s %s = %d %s, want %d", method, path, res.StatusCode, raw, wantStatus)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
}

func shortsDefinition() map[string]any {
	return map[string]any{
		"name":     "Psikoloji shorts",
		"language": "tr",
		"outputs": map[string]any{
			"video": map[string]any{"format": "short", "orientation": "vertical", "durationSeconds": 45},
		},
		"source":   map[string]any{"kind": "prompt", "instructions": "Kaygı ve stres üzerine eğitici kısa videolar."},
		"schedule": map[string]any{"cron": "0 2 * * *", "timezone": "Europe/Istanbul", "batch": 5},
	}
}

func TestPipelineLifecycle(t *testing.T) {
	srv := newServer(t)

	var ws gen.Workspace
	call(t, srv, "POST", "/api/v1/workspaces", map[string]any{"name": "acme", "timezone": "Europe/Istanbul"}, 201, &ws)
	base := "/api/v1/workspaces/" + ws.Id.String() + "/pipelines"

	var p gen.Pipeline
	call(t, srv, "POST", base, shortsDefinition(), 201, &p)
	if p.Status != "active" || p.NextRunAt == nil || p.Definition.Outputs.Lead == nil || *p.Definition.Outputs.Lead != "video" {
		t.Fatalf("created pipeline = %+v", p)
	}
	if p.Definition.Schedule == nil || p.Definition.Schedule.Timezone != "Europe/Istanbul" {
		t.Fatalf("schedule = %+v", p.Definition.Schedule)
	}

	var list struct{ Pipelines []gen.Pipeline }
	call(t, srv, "GET", base, nil, 200, &list)
	if len(list.Pipelines) != 1 {
		t.Fatalf("list = %d", len(list.Pipelines))
	}

	def := shortsDefinition()
	def["name"] = "Renamed"
	var updated gen.Pipeline
	call(t, srv, "PUT", base+"/"+p.Id.String(), map[string]any{"version": p.Version, "definition": def}, 200, &updated)
	if updated.Definition.Name != "Renamed" || updated.Version != p.Version+1 {
		t.Fatalf("updated = %+v", updated)
	}
	var stale gen.Error
	call(t, srv, "PUT", base+"/"+p.Id.String(), map[string]any{"version": p.Version, "definition": def}, 409, &stale)
	if stale.Code != "StaleVersion" {
		t.Fatalf("stale code = %s", stale.Code)
	}

	var paused gen.Pipeline
	call(t, srv, "POST", base+"/"+p.Id.String()+"/pause", nil, 200, &paused)
	if paused.Status != "paused" || paused.NextRunAt != nil {
		t.Fatalf("paused = %+v", paused)
	}
	var again gen.Error
	call(t, srv, "POST", base+"/"+p.Id.String()+"/pause", nil, 409, &again)

	var job gen.Job
	call(t, srv, "POST", base+"/"+p.Id.String()+"/runs", map[string]any{"count": 2}, 202, &job)
	if job.Kind != pipelinesvc.KindRun || job.Status != "queued" {
		t.Fatalf("job = %+v", job)
	}
	var jobs struct{ Jobs []gen.Job }
	call(t, srv, "GET", "/api/v1/workspaces/"+ws.Id.String()+"/jobs?status=queued", nil, 200, &jobs)
	if len(jobs.Jobs) != 1 || jobs.Jobs[0].Id != job.Id {
		t.Fatalf("jobs = %+v", jobs)
	}
	var cancelled gen.Job
	call(t, srv, "POST", "/api/v1/workspaces/"+ws.Id.String()+"/jobs/"+job.Id.String()+"/cancel", nil, 200, &cancelled)
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancelled = %+v", cancelled)
	}

	call(t, srv, "DELETE", base+"/"+p.Id.String(), nil, 204, nil)
	var missing gen.Error
	call(t, srv, "GET", base+"/"+p.Id.String(), nil, 404, &missing)
	if missing.Code != "PipelineNotFound" {
		t.Fatalf("missing code = %s", missing.Code)
	}
}

func TestValidationErrors(t *testing.T) {
	srv := newServer(t)
	var ws gen.Workspace
	call(t, srv, "POST", "/api/v1/workspaces", map[string]any{"name": "acme", "timezone": "Europe/Istanbul"}, 201, &ws)
	base := "/api/v1/workspaces/" + ws.Id.String() + "/pipelines"

	tests := []struct {
		name   string
		mutate func(d map[string]any)
	}{
		{"no outputs", func(d map[string]any) { d["outputs"] = map[string]any{} }},
		{"horizontal short", func(d map[string]any) {
			d["outputs"] = map[string]any{"video": map[string]any{"format": "short", "orientation": "horizontal", "durationSeconds": 45}}
		}},
		{"both outputs without lead", func(d map[string]any) {
			d["outputs"] = map[string]any{
				"video":   map[string]any{"format": "short", "orientation": "vertical", "durationSeconds": 45},
				"article": map[string]any{"words": 900, "translations": []string{"en"}},
			}
		}},
		{"every-minute cron", func(d map[string]any) {
			d["schedule"] = map[string]any{"cron": "* * * * *", "timezone": "Europe/Istanbul", "batch": 1}
		}},
		{"host-local timezone", func(d map[string]any) {
			d["schedule"] = map[string]any{"cron": "0 2 * * *", "timezone": "Local", "batch": 1}
		}},
		{"prompt source with refs", func(d map[string]any) {
			d["source"] = map[string]any{"kind": "prompt", "instructions": "x", "refs": []map[string]any{{"documentId": ws.Id.String()}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := shortsDefinition()
			tt.mutate(d)
			var e gen.Error
			call(t, srv, "POST", base, d, 400, &e)
			if e.Code != "ValidationFailed" || e.Message == "" {
				t.Fatalf("error = %+v", e)
			}
		})
	}

	var bad gen.Error
	call(t, srv, "POST", base, "not an object", 400, &bad)
	if bad.Code != "BadRequest" {
		t.Fatalf("malformed body code = %s", bad.Code)
	}
	var noWs gen.Error
	call(t, srv, "POST", "/api/v1/workspaces/00000000-0000-7000-8000-000000000000/pipelines", shortsDefinition(), 404, &noWs)
	if noWs.Code != "WorkspaceNotFound" {
		t.Fatalf("unknown workspace code = %s", noWs.Code)
	}
}

func TestSPAFallbackAndUnknownAPI(t *testing.T) {
	srv := newServer(t)
	res, err := http.Get(srv.URL + "/pipelines/some-client-route")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("<title>Kurgu</title>")) {
		t.Fatalf("spa fallback = %d %s", res.StatusCode, body)
	}
	var e gen.Error
	call(t, srv, "GET", "/api/v1/nope", nil, 404, &e)
	if e.Code != "NotFound" {
		t.Fatalf("unknown api code = %s", e.Code)
	}
}
