package production

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	storydomain "kurgu/internal/domain/story/v1"
)

// Every note of a round reaches the agent, resumed or not.
func TestDraftPromptListsEveryNote(t *testing.T) {
	at := 12.0
	fb, err := storydomain.NewFeedback([]storydomain.Note{
		{Text: "Wrong chart\nuse the 2024 figures", Scope: storydomain.ScopeVisuals, AtSeconds: &at},
		{Text: "Shorter overall"},
	}, uuid.Nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	one, _ := storydomain.NewFeedback([]storydomain.Note{{Text: "Slower voice", Scope: storydomain.ScopeVoice}}, uuid.Nil, time.Now())
	tests := []struct {
		name string
		task DraftTask
		want []string
		not  []string
	}{
		{
			name: "first draft",
			task: DraftTask{},
			want: []string{"Read INSTRUCTIONS.md"},
			not:  []string{"reviewer"},
		},
		{
			name: "revise in the session",
			task: DraftTask{Revision: 2, Resume: "sess", Feedback: &fb},
			want: []string{
				"revision 2 and asked for 2 changes; address every one",
				"1. The whole draft:\n   > Shorter overall\n",
				"2. The visuals, at about 12.0 s:\n   > Wrong chart\n   > use the 2024 figures\n",
				"note by note",
			},
		},
		{
			name: "one note",
			task: DraftTask{Revision: 1, Resume: "sess", Feedback: &one},
			want: []string{"asked for a change:", "1. The voice:\n   > Slower voice\n", "Apply the note"},
			not:  []string{"note by note"},
		},
		{
			name: "fresh draft without a session",
			task: DraftTask{Revision: 2, Feedback: &fb},
			want: []string{
				"Read INSTRUCTIONS.md",
				"address every one",
				"1. The whole draft:\n   > Shorter overall\n",
				"2. The visuals, at about 12.0 s:",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := draftPrompt(tt.task)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range tt.not {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q in:\n%s", w, got)
				}
			}
		})
	}
}
