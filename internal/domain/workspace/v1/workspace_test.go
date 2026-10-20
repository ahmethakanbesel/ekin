package workspace_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	workspace "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

func TestParseTimezone(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"istanbul", "Europe/Istanbul", false},
		{"utc", "UTC", false},
		{"trimmed", "  Europe/Berlin ", false},
		{"empty", "", true},
		{"local is host-dependent", "Local", true},
		{"unknown", "Mars/Olympus", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := workspace.ParseTimezone(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !apperror.Is(err, apperror.ValidationFailed) {
				t.Fatalf("code = %v, want ValidationFailed", err)
			}
		})
	}
}

func TestNewValidatesName(t *testing.T) {
	loc, _ := workspace.ParseTimezone("Europe/Istanbul")
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"ok", "acme", false},
		{"blank", "   ", true},
		{"max length", strings.Repeat("ş", workspace.MaxNameLength), false},
		{"too long", strings.Repeat("ş", workspace.MaxNameLength+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := workspace.New(id.New(), tt.in, loc, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	loc, _ := workspace.ParseTimezone("Europe/Istanbul")
	created := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	w, err := workspace.New(id.New(), "acme", loc, created)
	if err != nil {
		t.Fatal(err)
	}
	berlin, _ := workspace.ParseTimezone("Europe/Berlin")
	if err := w.ChangeTimezone(berlin, created.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := workspace.Rehydrate(w.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if got.Snapshot() != w.Snapshot() {
		t.Fatalf("round trip = %+v, want %+v", got.Snapshot(), w.Snapshot())
	}
}
