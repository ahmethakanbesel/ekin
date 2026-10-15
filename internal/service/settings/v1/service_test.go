package settings_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"kurgu/internal/apperror"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	settingssvc "kurgu/internal/service/settings/v1"
	"kurgu/pkg/id"
	"kurgu/pkg/secretbox"
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
	changed := svc.Changed()
	if _, err := svc.SaveProfile(ctx, p, id.New()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	default:
		t.Fatal("a save did not signal the change")
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
	for _, pct := range []int{5, 101, -1} {
		bad = p
		bad.CPULimit = pct
		if _, err := svc.SaveProfile(ctx, bad, id.New()); !apperror.Is(err, apperror.ValidationFailed) {
			t.Fatalf("cpu limit %d = %v", pct, err)
		}
	}
	limited := p
	limited.CPULimit = 50
	if _, err := svc.SaveProfile(ctx, limited, id.New()); err != nil {
		t.Fatalf("cpu limit 50 = %v", err)
	}
	if got, _ := svc.Profile(ctx, "vps"); got.CPULimit != 50 {
		t.Fatalf("stored cpu limit = %d", got.CPULimit)
	}
	changed = svc.Changed()
	bad = p
	bad.Concurrency = map[jobdomain.Queue]int{"gpu": 1}
	if _, err := svc.SaveProfile(ctx, bad, id.New()); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("bad queue = %v", err)
	}
	select {
	case <-changed:
		t.Fatal("a rejected save signalled a change")
	default:
	}
}

func TestCredentials(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	box, _ := secretbox.New([]byte("test-secret-0123456789"))
	env := settingsdomain.Credentials{PexelsAPIKey: "env-pexels-key-1234", DecisionModel: "typesafe/jev-1.13"}
	repo := settingsrepo.NewSQLite(tx)
	svc := settingssvc.NewService(repo, defaults(), slog.New(slog.DiscardHandler)).WithCredentials(env, box)

	if c := svc.Credentials(ctx); c.PexelsAPIKey != "env-pexels-key-1234" {
		t.Fatalf("env fallback = %+v", c)
	}
	if err := svc.UpdateCredentials(ctx, map[string]string{"pexelsApiKey": "ui-key-abcdef9876", "smtpPort": "587"}, id.New()); err != nil {
		t.Fatal(err)
	}
	if c := svc.Credentials(ctx); c.PexelsAPIKey != "ui-key-abcdef9876" || c.Port() != 587 || c.DecisionModel != "typesafe/jev-1.13" {
		t.Fatalf("merged = %+v", c)
	}
	var raw map[string]string
	_ = repo.Get(ctx, "credentials", &raw)
	if strings.Contains(raw["sealed"], "ui-key") {
		t.Fatal("credentials stored in clear")
	}
	statuses, _ := svc.CredentialStatuses(ctx)
	for _, st := range statuses {
		switch st.Field.Key {
		case "pexelsApiKey":
			if st.Source != "settings" || st.Value != "••••9876" {
				t.Fatalf("pexels status = %+v", st)
			}
		case "decisionModel":
			if st.Source != "env" || st.Value != "typesafe/jev-1.13" {
				t.Fatalf("model status = %+v", st)
			}
		}
	}
	if err := svc.UpdateCredentials(ctx, map[string]string{"pexelsApiKey": ""}, id.New()); err != nil {
		t.Fatal(err)
	}
	if c := svc.Credentials(ctx); c.PexelsAPIKey != "env-pexels-key-1234" {
		t.Fatalf("cleared value did not fall back to env: %+v", c)
	}
	for name, patch := range map[string]map[string]string{
		"unknown key": {"awsKey": "x"}, "bad port": {"smtpPort": "99999"}, "bad tls": {"smtpTls": "ssl"},
	} {
		if err := svc.UpdateCredentials(ctx, patch, id.New()); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("%s = %v", name, err)
		}
	}
}

func TestCredentialDefaultsAreNotReportedAsEnv(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	box, _ := secretbox.New([]byte("test-secret-0123456789"))
	tests := []struct {
		name       string
		env        settingsdomain.Credentials
		wantSource string
		wantValue  string
	}{
		{"built-in default", settingsdomain.Credentials{}, "default", "typesafe/jev-1.13"},
		{"set in the environment", settingsdomain.Credentials{DecisionModel: "other/model"}, "env", "other/model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := settingssvc.NewService(settingsrepo.NewSQLite(tx), defaults(), slog.New(slog.DiscardHandler)).
				WithCredentials(tt.env, box).
				WithCredentialDefaults(settingsdomain.Credentials{DecisionModel: "typesafe/jev-1.13", SMTPTLS: "starttls"})
			if c := svc.Credentials(ctx); c.DecisionModel != tt.wantValue || c.SMTPTLS != "starttls" {
				t.Fatalf("effective = %+v", c)
			}
			statuses, _ := svc.CredentialStatuses(ctx)
			for _, st := range statuses {
				if st.Field.Key == "decisionModel" && (st.Source != tt.wantSource || st.Value != tt.wantValue) {
					t.Fatalf("status = %+v", st)
				}
			}
		})
	}
}
