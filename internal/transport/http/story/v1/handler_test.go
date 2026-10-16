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
