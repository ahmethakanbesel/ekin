package defaults_test

import (
	"context"
	"log/slog"
	"testing"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	librarydomain "kurgu/internal/domain/library/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
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
	if got, err := svc.Get(ctx, ws); err != nil || got != (pipelinedomain.Production{}) {
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
			_, err := svc.Put(ctx, ws, tt.in, id.New())
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
			if got, err := svc.Get(ctx, ws); err != nil || got != tt.in {
				t.Fatalf("read back %+v, %v", got, err)
			}
		})
	}
	if got, _ := svc.Get(ctx, other); got != (pipelinedomain.Production{}) {
		t.Fatalf("another workspace sees %+v", got)
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
