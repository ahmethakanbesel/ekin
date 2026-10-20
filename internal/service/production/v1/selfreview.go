package production

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ahmethakanbesel/ekin/internal/adapter/claude"
)

// reviewSchema is the editor's structured answer.
var reviewSchema = json.RawMessage(`{
  "type": "object",
  "required": ["verdict", "issues"],
  "properties": {
    "verdict": {"type": "string", "enum": ["pass", "revise"]},
    "summary": {"type": "string"},
    "issues": {
      "type": "array",
      "maxItems": 10,
      "items": {
        "type": "object",
        "required": ["file", "problem", "fix"],
        "properties": {
          "file": {"type": "string"},
          "passage": {"type": "string"},
          "problem": {"type": "string"},
          "fix": {"type": "string"}
        }
      }
    }
  }
}`)

// verdict is what the editor found.
type verdict struct {
	Verdict string  `json:"verdict"`
	Summary string  `json:"summary"`
	Issues  []issue `json:"issues"`
}

type issue struct {
	File    string `json:"file"`
	Passage string `json:"passage"`
	Problem string `json:"problem"`
	Fix     string `json:"fix"`
}

var languageNames = map[string]string{"tr": "Turkish", "en": "English"}

// selfReview runs up to task.SelfReview rounds: a fresh agent that only
// reads reviews the draft, and the author fixes what it reports (then
// passes Ekin's checks again). It returns notes for the revision. An
// editor that cannot run ends the review without failing the draft.
func (s *Service) selfReview(ctx context.Context, task DraftTask, dir string, a *author) (string, error) {
	rounds := task.SelfReview
	if rounds <= 0 {
		return "", nil
	}
	var notes strings.Builder
	notes.WriteString("\n\nSelf-review:")
	for round := 1; round <= rounds; round++ {
		v, err := s.review(ctx, task, dir, round, rounds)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			s.log.WarnContext(ctx, "self-review failed", slog.Int("round", round), slog.Any("error", err))
			fmt.Fprintf(&notes, "\n- Round %d could not run: %s", round, firstLine(err.Error()))
			break
		}
		if v.Verdict != "revise" || len(v.Issues) == 0 {
			fmt.Fprintf(&notes, "\n- Round %d: passed.%s", round, sentence(v.Summary))
			break
		}
		fmt.Fprintf(&notes, "\n- Round %d: %d to fix.%s", round, len(v.Issues), sentence(v.Summary))
		for _, is := range v.Issues {
			fmt.Fprintf(&notes, "\n  - %s: %s", is.File, firstLine(is.Problem))
		}
		prompt, err := execute("self-fix.md", map[string]any{
			"Round": round, "Max": rounds, "Issues": v.Issues, "Video": task.Video && task.Stage != StageScript, "Tool": toolCmd,
		})
		if err != nil {
			return "", err
		}
		if err := s.turn(ctx, a, prompt); err != nil {
			return "", err
		}
		if err := s.settle(ctx, task, dir, a); err != nil {
			return "", err
		}
		fmt.Fprintf(&notes, "\n  Author: %s", firstLine(strings.TrimSpace(a.reply)))
		if round == rounds {
			notes.WriteString("\n- Round limit reached: the last fixes were not reviewed again.")
		}
	}
	return notes.String(), nil
}

// review asks a fresh, read-only agent for a verdict on the draft in dir.
func (s *Service) review(ctx context.Context, task DraftTask, dir string, round, rounds int) (verdict, error) {
	_, err := os.Stat(filepath.Join(dir, "WRITING.md"))
	lang := languageNames[task.Spec.Language]
	if lang == "" {
		lang = task.Spec.Language
	}
	prompt, err2 := execute("self-review.md", map[string]any{
		"Video": task.Video, "Script": task.Stage == StageScript, "Writing": err == nil, "Language": lang,
		"Round": round, "Max": rounds,
	})
	if err2 != nil {
		return verdict{}, err2
	}
	tools := []string{"Read", "Glob", "Grep"}
	res, err := s.agent.Run(ctx, claude.Request{
		Dir: dir, Prompt: prompt, Tools: tools, Allowed: tools, JSONSchema: reviewSchema, Timeout: s.cfg.AgentTimeout,
		Model: task.Agent.Model, Effort: string(task.Agent.Effort),
	})
	if err != nil {
		return verdict{}, err
	}
	var v verdict
	if err := json.Unmarshal(res.Structured, &v); err != nil {
		return verdict{}, fmt.Errorf("editor answer: %w", err)
	}
	return v, nil
}

// sentence renders an optional summary after a label.
func sentence(s string) string {
	if s = firstLine(strings.TrimSpace(s)); s == "" {
		return ""
	}
	return " " + s
}
