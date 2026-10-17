package template_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/adapter/claude"
	"ekin/internal/apperror"
	"ekin/internal/db/dbtest"
	jobdomain "ekin/internal/domain/job/v1"
	pipelinedomain "ekin/internal/domain/pipeline/v1"
	templatedomain "ekin/internal/domain/template/v1"
	workspacedomain "ekin/internal/domain/workspace/v1"
	jobrepo "ekin/internal/repository/job/v1"
	templaterepo "ekin/internal/repository/template/v1"
	workspacerepo "ekin/internal/repository/workspace/v1"
	jobsvc "ekin/internal/service/job/v1"
	templatesvc "ekin/internal/service/template/v1"
	"ekin/pkg/id"
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
	if _, err := svc.Update(ctx, w.ID(), cosmic.ID, 0, cosmic.Spec()); !apperror.Is(err, apperror.Forbidden) {
		t.Fatalf("edited a built-in: %v", err)
	}
	copyT, err := svc.Duplicate(ctx, w.ID(), cosmic.ID)
	if err != nil || copyT.Source != templatedomain.SourceCustom || copyT.Name != "Cosmic flat explainer (copy)" {
		t.Fatalf("duplicate = %+v %v", copyT, err)
	}
	spec := copyT.Spec()
	spec.Palette = []string{"#abcdef"}
	updated, err := svc.Update(ctx, w.ID(), copyT.ID, 0, spec)
	if err != nil || updated.Palette[0] != "#ABCDEF" {
		t.Fatalf("update = %+v %v", updated.Palette, err)
	}
	spec.HeadingFont = "Comic Sans"
	if _, err := svc.Update(ctx, w.ID(), copyT.ID, 0, spec); !apperror.Is(err, apperror.ValidationFailed) {
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

// racingRepo runs race once, right after the next Get: someone else's
// change landing between an update's read and its write.
type racingRepo struct {
	templaterepo.Repository
	race func()
}

func (r *racingRepo) Get(ctx context.Context, ws, id uuid.UUID) (templatedomain.Template, error) {
	t, err := r.Repository.Get(ctx, ws, id)
	if race := r.race; race != nil {
		r.race = nil
		race()
	}
	return t, err
}

// An update names the version it read: a newer edit is not overwritten,
// and a template deleted meanwhile is not brought back.
func TestTemplateUpdateIsOptimistic(t *testing.T) {
	tests := []struct {
		name    string
		race    func(t *testing.T, svc *templatesvc.Service, ws uuid.UUID, tmpl templatedomain.Template)
		version func(read int64) int64
		wantErr apperror.Code
	}{
		{name: "the version read", version: func(read int64) int64 { return read }},
		{name: "no version", version: func(int64) int64 { return 0 }},
		{name: "saved by someone after the caller read", race: edit, version: func(read int64) int64 { return read }, wantErr: apperror.StaleVersion},
		{name: "saved by someone while updating", race: edit, version: func(int64) int64 { return 0 }, wantErr: apperror.StaleVersion},
		{name: "deleted while updating", race: remove, version: func(int64) int64 { return 0 }, wantErr: apperror.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, tx := dbtest.Open(t)
			w, _ := workspacedomain.New(id.New(), "ws", time.UTC, time.Now())
			_ = workspacerepo.NewSQLite(tx).Create(ctx, w)
			repo := &racingRepo{Repository: templaterepo.NewSQLite(tx)}
			svc := templatesvc.NewService(repo, nil, nil, t.TempDir())
			tmpl, err := svc.Duplicate(ctx, w.ID(), templatedomain.BuiltinID("cosmic-flat"))
			if err != nil || tmpl.Version != 1 {
				t.Fatalf("duplicate = v%d %v", tmpl.Version, err)
			}
			if tt.race != nil {
				repo.race = func() { tt.race(t, svc, w.ID(), tmpl) }
			}
			spec := tmpl.Spec()
			spec.Name = "Mine"
			got, err := svc.Update(ctx, w.ID(), tmpl.ID, tt.version(tmpl.Version), spec)
			stored, getErr := templaterepo.NewSQLite(tx).Get(ctx, w.ID(), tmpl.ID)
			if tt.wantErr != "" {
				if !apperror.Is(err, tt.wantErr) {
					t.Fatalf("update = %v, want %s", err, tt.wantErr)
				}
				if getErr == nil && stored.Name == "Mine" {
					t.Fatalf("the refused update was stored (v%d)", stored.Version)
				}
				if tt.wantErr == apperror.NotFound && getErr == nil {
					t.Fatal("the deleted template came back")
				}
				return
			}
			if err != nil || got.Version != 2 || stored.Name != "Mine" || stored.Version != 2 {
				t.Fatalf("update = v%d %v; stored %q v%d", got.Version, err, stored.Name, stored.Version)
			}
		})
	}
}

func edit(t *testing.T, svc *templatesvc.Service, ws uuid.UUID, tmpl templatedomain.Template) {
	spec := tmpl.Spec()
	spec.Name = "Theirs"
	if _, err := svc.Update(context.Background(), ws, tmpl.ID, tmpl.Version, spec); err != nil {
		t.Fatal(err)
	}
}

func remove(t *testing.T, svc *templatesvc.Service, ws uuid.UUID, tmpl templatedomain.Template) {
	if err := svc.Delete(context.Background(), ws, tmpl.ID); err != nil {
		t.Fatal(err)
	}
}
