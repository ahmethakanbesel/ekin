package analytics_test

import (
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	analytics "github.com/ahmethakanbesel/ekin/internal/domain/analytics/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

var now = time.Date(2026, 10, 31, 9, 30, 0, 0, time.UTC)

func date(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

func TestProperty(t *testing.T) {
	for in, want := range map[string]string{
		"sc-domain:Example.com":  "sc-domain:example.com",
		"https://example.com/":   "https://example.com/",
		"https://example.com/b/": "https://example.com/b/",
	} {
		if got, err := analytics.ParseProperty(in); err != nil || got != want {
			t.Errorf("%q = %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "sc-domain:", "https://example.com", "ftp://x/", "example.com"} {
		if _, err := analytics.ParseProperty(bad); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("%q accepted", bad)
		}
	}
	cases := []struct {
		property, site string
		want           bool
	}{
		{"sc-domain:example.com", "https://www.example.com/", true},
		{"sc-domain:example.com", "https://blog.example.com/", true},
		{"sc-domain:example.com", "https://example.org/", false},
		{"https://example.com/", "https://example.com", true},
		{"https://example.com/blog/", "https://example.com/", false},
	}
	for _, c := range cases {
		if got := analytics.Matches(c.property, c.site); got != c.want {
			t.Errorf("Matches(%q, %q) = %v", c.property, c.site, got)
		}
	}
}

func TestConnectionWindow(t *testing.T) {
	c, err := analytics.Connect(id.New(), "sealed", id.New(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Window(now); ok || c.Due(now) {
		t.Fatal("a connection without a property syncs")
	}
	if changed, err := c.Choose("sc-domain:example.com", now); err != nil || !changed {
		t.Fatalf("choose = %v %v", changed, err)
	}
	start, end, ok := c.Window(now)
	if !ok || !end.Equal(date(10, 30)) || !start.Equal(date(10, 31).Add(-analytics.Backfill)) || !c.Due(now) {
		t.Fatalf("first window %v–%v %v", start, end, ok)
	}
	c.Synced(end, now)
	if c.Due(now.Add(time.Hour)) || !c.Due(now.Add(analytics.SyncEvery)) {
		t.Fatal("due too early or never")
	}
	start, end, ok = c.Window(now.Add(24 * time.Hour))
	if !ok || !start.Equal(date(10, 28)) || !end.Equal(date(10, 31)) {
		t.Fatalf("next window %v–%v (refetching the last three days)", start, end)
	}
	if changed, _ := c.Choose("sc-domain:example.com", now); changed {
		t.Fatal("the same property counts as a change")
	}
	_, _ = c.Choose("https://example.com/", now)
	if !c.SyncedThrough().IsZero() {
		t.Fatal("another property keeps the old sync state")
	}
	c.Failed("token revoked", now)
	back, err := analytics.Rehydrate(c.Snapshot())
	if err != nil || back.LastError() != "token revoked" || back.Property() != "https://example.com/" {
		t.Fatalf("rehydrate = %v", err)
	}
}

func TestCompareAndMovers(t *testing.T) {
	var ms []analytics.Metric
	for d := 1; d <= 30; d++ {
		clicks := 2
		if d >= 20 {
			clicks = 6 // published on the 20th
		}
		ms = append(ms, analytics.Metric{Date: date(10, d), Key: "https://example.com/a", Clicks: clicks, Impressions: 100, Position: float64(30 - d)})
	}
	c := analytics.Compare(ms, date(10, 20).Add(15*time.Hour), date(10, 30))
	if c.Before.Days != 28 || c.Before.Clicks != 19*2 || c.After.Days != 11 || c.After.Clicks != 11*6 {
		t.Fatalf("compare = %+v", c)
	}
	if c.After.ClicksPerDay() != 6 || c.After.Position != 5 { // average of 10..0
		t.Fatalf("after per day %v, position %v", c.After.ClicksPerDay(), c.After.Position)
	}
	ms = append(ms, analytics.Metric{Date: date(10, 29), Key: "https://example.com/quiet", Clicks: 1, Impressions: 5})
	moves := analytics.Movers(ms, date(10, 30), 7, 10, 5)
	if len(moves) != 1 || moves[0].Key != "https://example.com/a" || moves[0].Delta() != 7*6-(3*2+4*6) {
		t.Fatalf("movers = %+v", moves)
	}
}
