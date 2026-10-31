package httptransport_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/adapter/openrouter"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	visibilitysvc "github.com/ahmethakanbesel/ekin/internal/service/visibility/v1"
)

// fakeModels answers every prompt naming Globex first, then Acme.
type fakeModels struct{}

func (fakeModels) Enabled() bool { return true }
func (fakeModels) Ask(context.Context, string, string) (openrouter.Answer, error) {
	return openrouter.Answer{Text: "Globex is popular; Acme suits small teams.", Citations: []string{"https://acme.com/about"}}, nil
}

func TestVisibilityAPI(t *testing.T) {
	srv := newServer(t)
	owner := srv.signedIn(t, "owner@example.com")
	ws := owner.workspace("acme")
	base := "/api/v1/workspaces/" + ws.Id.String() + "/visibility"

	var o gen.VisibilityOverview
	owner.call("GET", base, nil, 200, &o)
	if !o.Ready || o.Settings != nil || len(o.Suggested.Models) == 0 {
		t.Fatalf("before = %+v", o)
	}
	owner.fail("POST", base+"/runs", nil, 400, "ValidationFailed")
	owner.fail("PUT", base, map[string]any{"brand": map[string]any{"name": "Acme"}, "competitors": []any{}, "models": []string{"no model"}}, 400, "ValidationFailed")
	owner.call("PUT", base, map[string]any{
		"brand": map[string]any{"name": "Acme", "domain": "acme.com"}, "competitors": []any{map[string]any{"name": "Globex"}},
		"models": []string{"perplexity/sonar", "openai/gpt-4o-mini:online"}, "monthlyCap": 10,
	}, 200, &o)
	if o.Settings == nil || o.Settings.Brand.Name != "Acme" || o.NextRunAt == nil {
		t.Fatalf("saved = %+v", o)
	}
	var p gen.VisibilityPrompt
	owner.call("POST", base+"/prompts", map[string]any{"text": "best tools for small teams"}, 201, &p)
	owner.fail("POST", base+"/prompts", map[string]any{"text": "Best tools for small teams"}, 409, "Conflict")
	owner.call("GET", base, nil, 200, &o)
	if o.Estimate.Calls != 2 || o.Estimate.Cap != 10 {
		t.Fatalf("estimate = %+v", o.Estimate)
	}
	var run gen.VisibilityRun
	owner.call("POST", base+"/runs", nil, 201, &run)
	if run.Planned != 2 || run.Status != gen.VisibilityRunStatusQueued {
		t.Fatalf("run = %+v", run)
	}
	ctx := context.Background()
	j, err := srv.jobs.Claim(ctx, jobdomain.Queues, []string{visibilitysvc.KindRun}, "test", time.Minute)
	if err != nil || j == nil {
		t.Fatalf("claim = %v", err)
	}
	var payload visibilitysvc.RunPayload
	_ = json.Unmarshal(j.Payload(), &payload)
	if err := srv.visibility.RunCheck(ctx, payload); err != nil {
		t.Fatal(err)
	}
	var rep gen.VisibilityReport
	owner.call("GET", base+"/report", nil, 200, &rep)
	if rep.Run == nil || rep.Run.Done != 2 || len(rep.Answers) != 2 || rep.Answers[0].Position != 2 || !rep.Answers[0].Cited ||
		rep.Voices[0].Name != "Acme" || rep.Voices[1].Mentions != 2 {
		t.Fatalf("report = %+v", rep)
	}
	owner.call("PATCH", base+"/prompts/"+p.Id.String(), map[string]any{"active": false}, 200, &p)
	if p.Active {
		t.Fatal("prompt still active")
	}
	owner.call("DELETE", base+"/prompts/"+p.Id.String(), nil, 204, nil)
}
