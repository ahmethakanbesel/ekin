package pipeline_test

import (
	"testing"
	"time"

	pipeline "ekin/internal/domain/pipeline/v1"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestParseSchedule(t *testing.T) {
	ist := mustLoc(t, "Europe/Istanbul")
	tests := []struct {
		name    string
		expr    string
		loc     *time.Location
		batch   int
		wantErr bool
	}{
		{"nightly", "0 2 * * *", ist, 5, false},
		{"descriptor", "@daily", ist, 1, false},
		{"weekdays twice", "0 2,14 * * 1-5", ist, 3, false},
		{"hourly is the floor", "0 * * * *", ist, 1, false},
		{"every minute rejected", "* * * * *", ist, 1, true},
		{"half-hourly rejected", "0,30 * * * *", ist, 1, true},
		{"every rejected", "@every 2h", ist, 1, true},
		{"inline timezone rejected", "CRON_TZ=UTC 0 2 * * *", ist, 1, true},
		{"seconds field rejected", "0 0 2 * * *", ist, 1, true},
		{"garbage", "nope", ist, 1, true},
		{"empty", "  ", ist, 1, true},
		{"no timezone", "0 2 * * *", nil, 1, true},
		{"zero batch", "0 2 * * *", ist, 0, true},
		{"batch over max", "0 2 * * *", ist, pipeline.MaxBatch + 1, true},
		{"never fires", "0 0 30 2 *", ist, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.ParseSchedule(tt.expr, tt.loc, tt.batch)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestScheduleNextUsesTimezone(t *testing.T) {
	tests := []struct {
		name  string
		tz    string
		after time.Time
		want  time.Time
	}{
		{
			name:  "istanbul 02:00 is 23:00 UTC",
			tz:    "Europe/Istanbul",
			after: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
			want:  time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC),
		},
		{
			name:  "berlin summer time",
			tz:    "Europe/Berlin",
			after: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC),
			want:  time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "berlin after the october change stays at 02:00 local",
			tz:    "Europe/Berlin",
			after: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
			want:  time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := pipeline.ParseSchedule("0 2 * * *", mustLoc(t, tt.tz), 1)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Next(tt.after); !got.Equal(tt.want) {
				t.Fatalf("Next = %v, want %v", got, tt.want)
			}
		})
	}
}
