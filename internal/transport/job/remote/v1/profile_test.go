package worker_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kurgu/internal/adapter/remote"
	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	settingssvc "kurgu/internal/service/settings/v1"
	workerapi "kurgu/internal/transport/http/worker"
	"kurgu/pkg/id"
)

// A remote worker keeps its own profile until an admin saves one of
// that name on the server, then follows every save, and keeps the last
// one it knew while the server is unreachable.
func TestRemoteWorkerFollowsTheSavedProfile(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	_, tx := dbtest.Open(t)
	server := settingsdomain.Profile{
		Name: "default", Concurrency: map[jobdomain.Queue]int{jobdomain.QueueIO: 2},
		RenderDuty: 1, RenderChunks: 1, PreviewScale: 0.5, FinalScale: 1,
	}
	settings := settingssvc.NewService(settingsrepo.NewSQLite(tx), tx, server, log)
	const token = "a-worker-token-of-enough-length"
	mux := http.NewServeMux()
	workerapi.Register(mux, token, nil, nil, settings, log, func(w http.ResponseWriter, _ *http.Request, err error) {
		code, _ := apperror.GetCode(err)
		w.WriteHeader(apperror.StatusFor(code))
		_ = json.NewEncoder(w).Encode(map[string]string{"code": string(code), "message": err.Error()})
	})
	srv := httptest.NewServer(mux)

	local := settingsdomain.Profile{
		Name: "mac", Concurrency: map[jobdomain.Queue]int{jobdomain.QueueAgent: 1, jobdomain.QueueRender: 1},
		HeavyExclusive: true, Cooldown: time.Minute, RenderDuty: 0.5, RenderChunks: 2, PreviewScale: 0.5, FinalScale: 1,
	}
	w := remote.NewProfileWatcher(&remote.Client{BaseURL: srv.URL, Token: token, Owner: "mac"}, local, time.Hour, log)
	changed := w.Changed()
	w.Refresh(ctx)
	if got := w.Throttle(ctx); got.Concurrency[jobdomain.QueueAgent] != 1 || got.Cooldown != time.Minute {
		t.Fatalf("unsaved: throttle = %+v, want the local one", got)
	}
	select {
	case <-changed:
		t.Fatal("no change signalled a change")
	default:
	}

	saved := local
	saved.Paused, saved.Cooldown, saved.RenderDuty = true, 0, 0.25
	if _, err := settings.SaveProfile(ctx, saved, id.New()); err != nil {
		t.Fatal(err)
	}
	w.Refresh(ctx)
	select {
	case <-changed:
	default:
		t.Fatal("a saved profile did not signal a change")
	}
	if got := w.Throttle(ctx); !got.Paused || got.Cooldown != 0 || w.Profile(ctx).RenderDuty != 0.25 {
		t.Fatalf("saved: throttle = %+v", got)
	}

	srv.Close()
	w.Refresh(ctx)
	if !w.Throttle(ctx).Paused {
		t.Fatal("an unreachable server reset the profile")
	}
}
