package defaults_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	librarydomain "kurgu/internal/domain/library/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	storydomain "kurgu/internal/domain/story/v1"
	templatedomain "kurgu/internal/domain/template/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	defaultssvc "kurgu/internal/service/defaults/v1"
	settingssvc "kurgu/internal/service/settings/v1"
	"kurgu/pkg/id"
	"kurgu/pkg/secretbox"
)

func TestGetAndPut(t *testing.T) {
	ctx := context.Background()
	svc := defaultssvc.NewService(settingsrepo.NewInMemory(), nil, defaultssvc.Config{})
	ws, other := id.New(), id.New()
	if got, err := svc.Get(ctx, ws); err != nil || got != (defaultssvc.Workspace{}) {
		t.Fatalf("unset = %+v, %v", got, err)
	}
	tests := []struct {
		name    string
		in      pipelinedomain.Production
		wantErr bool
	}{
		{name: "everything", in: pipelinedomain.Production{
			Agent:          pipelinedomain.Agent{Model: "opus", Effort: pipelinedomain.EffortHigh},
			PreviewQuality: pipelinedomain.Quality720p, FinalQuality: pipelinedomain.Quality1440p, PreviewFPS: 24, FinalFPS: 60,
			ReviewAt: pipelinedomain.ReviewScript, Media: librarydomain.Policy{Music: librarydomain.ModeLibrary},
			Thumbnails: pipelinedomain.ThumbnailsOff, SelfReview: 3}},
		{name: "self-review off", in: pipelinedomain.Production{SelfReview: pipelinedomain.SelfReviewOff}},
		{name: "nothing", in: pipelinedomain.Production{}},
		{name: "bad final quality", in: pipelinedomain.Production{FinalQuality: "360p"}, wantErr: true},
		{name: "too many rounds", in: pipelinedomain.Production{SelfReview: 6}, wantErr: true},
		{name: "unknown media mode", in: pipelinedomain.Production{Media: librarydomain.Policy{Photo: "all"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := svc.Get(ctx, ws)
			_, err := svc.Put(ctx, ws, defaultssvc.Workspace{Production: tt.in}, id.New())
			if tt.wantErr {
				if !apperror.Is(err, apperror.ValidationFailed) {
					t.Fatalf("err = %v, want ValidationFailed", err)
				}
				if got, _ := svc.Get(ctx, ws); got != before {
					t.Fatalf("a rejected save changed the defaults: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := svc.Get(ctx, ws); err != nil || got != (defaultssvc.Workspace{Production: tt.in}) {
				t.Fatalf("read back %+v, %v", got, err)
			}
		})
	}
	if got, _ := svc.Get(ctx, other); got != (defaultssvc.Workspace{}) {
		t.Fatalf("another workspace sees %+v", got)
	}
}

// fakeTemplates knows one workspace template; built-ins resolve anywhere.
type fakeTemplates struct{ workspace, own uuid.UUID }

func (f fakeTemplates) Style(_ context.Context, workspaceID, templateID uuid.UUID) (*storydomain.TemplateStyle, error) {
	if templateID == templatedomain.BuiltinID("evidence-guide") || (workspaceID == f.workspace && templateID == f.own) {
		return &storydomain.TemplateStyle{ID: templateID}, nil
	}
	return nil, apperror.New(apperror.NotFound, "template not found")
}

func TestPutDefaultTemplate(t *testing.T) {
	ctx := context.Background()
	ws, other, own := id.New(), id.New(), id.New()
	svc := defaultssvc.NewService(settingsrepo.NewInMemory(), nil, defaultssvc.Config{}).
		WithTemplates(fakeTemplates{workspace: ws, own: own})
	tests := []struct {
		name      string
		workspace uuid.UUID
		template  uuid.UUID
		wantErr   bool
	}{
		{name: "the workspace's own template", workspace: ws, template: own},
		{name: "a built-in", workspace: ws, template: templatedomain.BuiltinID("evidence-guide")},
		{name: "none", workspace: ws},
		{name: "an unknown template", workspace: ws, template: id.New(), wantErr: true},
		{name: "another workspace's template", workspace: other, template: own, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := defaultssvc.Workspace{Production: pipelinedomain.Production{SelfReview: 2}, TemplateID: tt.template}
			_, err := svc.Put(ctx, tt.workspace, in, id.New())
			if tt.wantErr {
				if !apperror.Is(err, apperror.ValidationFailed) {
					t.Fatalf("err = %v, want ValidationFailed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := svc.Get(ctx, tt.workspace); err != nil || got != in {
				t.Fatalf("read back %+v, %v", got, err)
			}
		})
	}
}

// Defaults saved before the default template joined them read back
// unchanged: the stored JSON keeps the Production fields at the top.
func TestGetReadsDefaultsSavedBeforeTemplates(t *testing.T) {
	ctx := context.Background()
	repo := settingsrepo.NewInMemory()
	ws := id.New()
	old := pipelinedomain.Production{FinalQuality: pipelinedomain.Quality1440p, SelfReview: pipelinedomain.SelfReviewOff}
	if err := repo.Put(ctx, "production:"+ws.String(), old, id.New(), time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := defaultssvc.NewService(repo, nil, defaultssvc.Config{}).Get(ctx, ws)
	if err != nil || got != (defaultssvc.Workspace{Production: old}) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

func TestServer(t *testing.T) {
	ctx := context.Background()
	profile := settingsdomain.Profile{Name: "mac", RenderDuty: 1, RenderChunks: 1, PreviewScale: 2.0 / 3, FinalScale: 1}
	box, _ := secretbox.New([]byte("test-secret-0123456789"))
	tests := []struct {
		name     string
		env      settingsdomain.Credentials
		saved    string // model saved on the settings page
		effort   string
		previewF int
		want     defaultssvc.Server
	}{
		{name: "nothing set", want: defaultssvc.Server{DraftModelSource: defaultssvc.ModelUnset,
			PreviewHeight: 720, FinalHeight: 1080, PreviewFPS: 30, FinalFPS: 30, Profile: "mac"}},
		{name: "model from the environment", env: settingsdomain.Credentials{ClaudeModel: "sonnet"}, effort: "high", previewF: 24,
			want: defaultssvc.Server{DraftModel: "sonnet", DraftModelSource: defaultssvc.ModelFromEnv, DraftEffort: pipelinedomain.EffortHigh,
				PreviewHeight: 720, FinalHeight: 1080, PreviewFPS: 24, FinalFPS: 30, Profile: "mac"}},
		{name: "settings page over the environment", env: settingsdomain.Credentials{ClaudeModel: "sonnet"}, saved: "opus",
			want: defaultssvc.Server{DraftModel: "opus", DraftModelSource: defaultssvc.ModelFromSettings,
				PreviewHeight: 720, FinalHeight: 1080, PreviewFPS: 30, FinalFPS: 30, Profile: "mac"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, tx := dbtest.Open(t)
			p := profile
			p.PreviewFPS = tt.previewF
			settings := settingssvc.NewService(settingsrepo.NewSQLite(tx), tx, p, slog.New(slog.DiscardHandler)).WithCredentials(tt.env, box)
			if tt.saved != "" {
				if err := settings.UpdateCredentials(ctx, map[string]string{"claudeModel": tt.saved}, id.New()); err != nil {
					t.Fatal(err)
				}
			}
			svc := defaultssvc.NewService(settingsrepo.NewSQLite(tx), settings, defaultssvc.Config{Profile: "mac", Effort: tt.effort})
			got, err := svc.Server(ctx)
			if err != nil || got != tt.want {
				t.Fatalf("Server() = %+v, %v\nwant %+v", got, err, tt.want)
			}
		})
	}
}
