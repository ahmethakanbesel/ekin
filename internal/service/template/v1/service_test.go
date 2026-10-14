package template_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	templatedomain "kurgu/internal/domain/template/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	templaterepo "kurgu/internal/repository/template/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	jobsvc "kurgu/internal/service/job/v1"
	templatesvc "kurgu/internal/service/template/v1"
	"kurgu/pkg/id"
)

type agent struct{ prompt *string }

func (a agent) Run(_ context.Context, req claude.Request) (claude.Result, error) {
	*a.prompt = req.Prompt
	b, _ := json.Marshal(map[string]any{
		"name": "Soft science sketches", "description": "d", "guide": "## Look\n- chalk on slate",
		"palette": []string{"#112233", "nope"}, "headingFont": "Caveat", "bodyFont": "Inter", "musicMood": "calm",
	})
	return claude.Result{Structured: b}, nil
}

func TestTemplates(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
	_ = workspacerepo.NewSQLite(tx).Create(ctx, w)
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	var prompt string
	svc := templatesvc.NewService(templaterepo.NewSQLite(tx), jobs, agent{&prompt}, t.TempDir())

	all, err := svc.List(ctx, w.ID())
	if err != nil || len(all) < 10 {
		t.Fatalf("built-ins = %d, %v", len(all), err)
	}
	for _, b := range all {
		if b.Source != templatedomain.SourceBuiltin {
			t.Fatalf("bad built-in %+v", b.Name)
		}
		if strings.Contains(strings.ToLower(b.Name), "kurzgesagt") || strings.Contains(strings.ToLower(b.Name), "vox") {
			t.Fatalf("built-in named after a channel: %s", b.Name)
		}
	}
	cosmic := find(t, all, "Cosmic flat explainer")
	if cosmic.ID != templatedomain.BuiltinID("cosmic-flat") {
		t.Fatal("built-in ids must be stable across releases")
	}
	if cosmic.HeadingFont != "Nunito" || len(cosmic.Palette) == 0 || !strings.Contains(cosmic.Guide, "## Motion") {
		t.Fatalf("cosmic = %+v", cosmic)
	}
	if err := svc.Delete(ctx, w.ID(), cosmic.ID); !apperror.Is(err, apperror.Forbidden) {
		t.Fatalf("deleted a built-in: %v", err)
	}
	if _, err := svc.Update(ctx, w.ID(), cosmic.ID, cosmic.Spec()); !apperror.Is(err, apperror.Forbidden) {
		t.Fatalf("edited a built-in: %v", err)
	}
	copyT, err := svc.Duplicate(ctx, w.ID(), cosmic.ID)
	if err != nil || copyT.Source != templatedomain.SourceCustom || copyT.Name != "Cosmic flat explainer (copy)" {
		t.Fatalf("duplicate = %+v %v", copyT, err)
	}
	spec := copyT.Spec()
	spec.Palette = []string{"#abcdef"}
	updated, err := svc.Update(ctx, w.ID(), copyT.ID, spec)
	if err != nil || updated.Palette[0] != "#ABCDEF" {
		t.Fatalf("update = %+v %v", updated.Palette, err)
	}
	spec.HeadingFont = "Comic Sans"
	if _, err := svc.Update(ctx, w.ID(), copyT.ID, spec); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("unknown font = %v", err)
	}
	if err := svc.Check(ctx, w.ID(), cosmic.ID, []pipelinedomain.Kind{pipelinedomain.KindArticle}); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("video template for an article = %v", err)
	}
	if err := svc.Check(ctx, id.New(), copyT.ID, []pipelinedomain.Kind{pipelinedomain.KindVideo}); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("other workspace's template = %v", err)
	}

	if _, err := svc.QueueGenerate(ctx, w.ID(), "Kurzgesagt", "more muted", nil); err != nil {
		t.Fatal(err)
	}
	j, _ := jobs.Claim(ctx, []jobdomain.Queue{jobdomain.QueueAgent}, []string{templatesvc.KindGenerate}, "t", time.Minute)
	var p templatesvc.GeneratePayload
	_ = json.Unmarshal(j.Payload(), &p)
	if err := svc.Generate(ctx, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, `"Kurzgesagt"`) || !strings.Contains(prompt, "never copy") || !strings.Contains(prompt, "more muted") {
		t.Fatalf("prompt = %s", prompt[:200])
	}
	all, _ = svc.List(ctx, w.ID())
	gen := find(t, all, "Soft science sketches")
	if gen.Source != templatedomain.SourceGenerated || len(gen.Palette) != 1 || gen.Inspiration == "" {
		t.Fatalf("generated = %+v", gen)
	}
}

func find(t *testing.T, all []templatedomain.Template, name string) templatedomain.Template {
	t.Helper()
	for _, x := range all {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no template %q", name)
	return templatedomain.Template{}
}
