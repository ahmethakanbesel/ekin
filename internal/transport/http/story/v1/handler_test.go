package handler

import (
	"testing"
	"time"

	"github.com/google/uuid"

	storydomain "kurgu/internal/domain/story/v1"
)

func TestFeedbackToWireNamesTheAuthor(t *testing.T) {
	author := uuid.New()
	at := 21.5
	tests := []struct {
		name string
		by   uuid.UUID
		want *uuid.UUID
	}{
		{name: "written by a person", by: author, want: &author},
		{name: "no author stored", by: uuid.Nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb, err := storydomain.NewFeedback("Slow the voice down", storydomain.ScopeVoice, &at, tt.by, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			got := feedbackToWire(&fb)
			switch {
			case tt.want == nil && got.By != nil:
				t.Fatalf("by = %v, want none", *got.By)
			case tt.want != nil && (got.By == nil || *got.By != *tt.want):
				t.Fatalf("by = %v, want %v", got.By, *tt.want)
			}
		})
	}
}

func TestMeasuresToWireLeavesOutWhatWasNotMeasured(t *testing.T) {
	if got := measuresToWire(storydomain.Measures{}); got != nil {
		t.Fatalf("unmeasured = %+v, want nil", got)
	}
	words := measuresToWire(storydomain.Measures{Words: 140})
	if words == nil || words.DurationSeconds != nil || words.Words == nil || *words.Words != 140 {
		t.Fatalf("words only = %+v", words)
	}
	both := measuresToWire(storydomain.Measures{Duration: 47520 * time.Millisecond, Words: 98})
	if both == nil || both.DurationSeconds == nil || *both.DurationSeconds != 47.52 || *both.Words != 98 {
		t.Fatalf("both = %+v", both)
	}
}
