package settings_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	settingssvc "kurgu/internal/service/settings/v1"
	"kurgu/pkg/id"
)

func defaults() settingsdomain.Profile {
	return settingsdomain.Profile{
		Name: "default", Concurrency: map[jobdomain.Queue]int{jobdomain.QueueIO: 2}, HeavyExclusive: true,
		Cooldown: 30 * time.Second, RenderDuty: 1, RenderChunks: 1, PreviewScale: 0.5, FinalScale: 1,
	}
}

func TestProfiles(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	svc := settingssvc.NewService(settingsrepo.NewSQLite(tx), defaults(), slog.New(slog.DiscardHandler))

	p, err := svc.Profile(ctx, "vps")
	if err != nil || p.Name != "vps" || p.Concurrency[jobdomain.QueueIO] != 2 {
		t.Fatalf("defaults = %+v, %v", p, err)
	}
	p.Concurrency = map[jobdomain.Queue]int{jobdomain.QueueRender: 1}
	p.RenderDuty, p.Paused = 0.25, true
	if _, err := svc.SaveProfile(ctx, p, id.New()); err != nil {
		t.Fatal(err)
	}
	got := svc.ThrottleSource("vps")(ctx)
	if !got.Paused || got.Concurrency[jobdomain.QueueRender] != 1 || got.Concurrency[jobdomain.QueueIO] != 0 {
		t.Fatalf("throttle = %+v", got)
	}
	list, _ := svc.Profiles(ctx, "mac")
	if len(list) != 2 || list[0].Name != "mac" || list[1].RenderDuty != 0.25 {
		t.Fatalf("profiles = %+v", list)
	}
	bad := p
	bad.RenderDuty = 0
	if _, err := svc.SaveProfile(ctx, bad, id.New()); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("bad duty = %v", err)
	}
	bad = p
	bad.Concurrency = map[jobdomain.Queue]int{"gpu": 1}
	if _, err := svc.SaveProfile(ctx, bad, id.New()); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("bad queue = %v", err)
	}
}
