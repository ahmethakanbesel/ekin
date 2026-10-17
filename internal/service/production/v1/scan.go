package production

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"ekin/internal/adapter/claude"
	"ekin/internal/apperror"
	jobdomain "ekin/internal/domain/job/v1"
	storydomain "ekin/internal/domain/story/v1"
	"ekin/pkg/id"
)

// KindMoments is the job that has the agent find moments in a long
// video that stand alone as shorts.
const KindMoments = "story.moments"

// MomentsPayload is a story.moments job: the long video to scan and how
// many shorts to make from the best moments (0 only suggests them).
type MomentsPayload struct {
	StoryID  uuid.UUID `json:"storyId"`
	SourceID uuid.UUID `json:"sourceId"`
	Make     int       `json:"make,omitempty"`
}

// scriptFile is the video's script in its work dir.
const scriptFile = "script.md"

// Candidates asked of the agent: twice what is made, at least this
// many when it only suggests.
const minCandidates = 8

// ScanMoments queues the agent to suggest moments of a long video and
// returns the job.
func (s *Service) ScanMoments(ctx context.Context, workspaceID, storyID, sourceID uuid.UUID) (*jobdomain.Job, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, err
	}
	if _, err := st.CanCut(sourceID, s.cfg.Shorts); err != nil {
		return nil, err
	}
	return s.enqueueScan(ctx, st, MomentsPayload{StoryID: storyID, SourceID: sourceID}, "")
}

func (s *Service) enqueueScan(ctx context.Context, st *storydomain.Story, p MomentsPayload, unique string) (*jobdomain.Job, error) {
	payload, _ := json.Marshal(p)
	j, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: st.WorkspaceID(), Queue: jobdomain.QueueControl, Kind: KindMoments, Payload: payload,
		UniqueKey: unique, ConcurrencyKey: KindMoments + ":" + p.SourceID.String(), MaxAttempts: 2, TraceID: st.TraceID(),
	})
	return j, err
}

// FindMoments runs a story.moments job: the agent reads the long
// video's script and numbered sentences and proposes moments, which
// replace its earlier suggestions; with Make set, the best become
// shorts at once.
func (s *Service) FindMoments(ctx context.Context, p MomentsPayload) error {
	st, err := s.stories.GetByID(ctx, p.StoryID)
	if apperror.Is(err, apperror.StoryNotFound) {
		return jobdomain.Permanent(err)
	}
	if err != nil {
		return err
	}
	src, err := st.CanCut(p.SourceID, s.cfg.Shorts)
	if err != nil {
		return jobdomain.Permanent(err)
	}
	sentences, err := s.sentences(ctx, src)
	if err != nil {
		return jobdomain.Permanent(err)
	}
	files, err := s.revisionFiles(ctx, src, scriptFile, metadataFile)
	if err != nil {
		return err
	}
	want := max(minCandidates, 2*p.Make)
	prompt := momentsPrompt(st, sentences, files, s.cfg.Shorts, want)
	agent := st.Brief().Production.Agent
	res, err := s.agent.Run(ctx, claude.Request{
		Dir: s.cfg.WorkRoot, Prompt: prompt, JSONSchema: momentsSchema, Timeout: 10 * time.Minute,
		Model: agent.Model, Effort: string(agent.Effort),
	})
	if err != nil {
		return fmt.Errorf("moments: %w", err)
	}
	drafts, err := parseMoments(res.Structured, sentences)
	if err != nil {
		return err
	}
	_, err = s.mutate(ctx, p.StoryID, func(ctx context.Context, st *storydomain.Story) error {
		kept, err := st.SuggestMoments(p.SourceID, drafts, segments(sentences), s.cfg.Shorts, id.New, s.now())
		if err != nil {
			return err
		}
		room := s.cfg.Shorts.MaxPerVideo() - len(st.Shorts(p.SourceID))
		n := min(p.Make, room, len(kept))
		if n <= 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, n)
		for _, m := range kept[:n] {
			ids = append(ids, m.ID())
		}
		made, err := st.AddShorts(p.SourceID, ids, s.cfg.Shorts, id.New, s.now())
		if err != nil {
			return err
		}
		for _, d := range made {
			if err := s.enqueueStep(ctx, st, d); err != nil {
				return err
			}
		}
		return nil
	})
	if apperror.Is(err, apperror.InvalidTransition) || apperror.Is(err, apperror.ValidationFailed) {
		return jobdomain.Permanent(err)
	}
	return err
}

var momentsSchema = json.RawMessage(`{
  "type": "object",
  "required": ["moments"],
  "properties": {
    "moments": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["first", "last", "hook", "reason", "score"],
        "properties": {
          "first": {"type": "integer", "description": "number of the moment's first sentence"},
          "last": {"type": "integer", "description": "number of its last sentence"},
          "hook": {"type": "string", "description": "the short's working title in the content language, at most 80 characters"},
          "reason": {"type": "string", "description": "one or two sentences: why it stands alone and grabs in its first seconds"},
          "score": {"type": "integer", "minimum": 0, "maximum": 100, "description": "how strong a short it makes"}
        }
      }
    }
  }
}`)

// momentsPrompt asks for want candidate moments of the long video, by
// sentence number.
func momentsPrompt(st *storydomain.Story, sentences []Sentence, files map[string][]byte, l storydomain.ShortsLimits, want int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You edit a %s-language YouTube channel. Find the %d best moments in this long video to cut into "+
		"vertical Shorts, as JSON.\n\n", languageName(st.Language()), want)
	b.WriteString("A moment is a run of whole sentences of the narration below, by number. Each short speaks its " +
		"moment word for word over new visuals, so a good moment:\n\n")
	fmt.Fprintf(&b, "- lasts %.0f–%.0f seconds (add up the sentence times);\n", l.ClipMin().Seconds(), l.ClipMax().Seconds())
	b.WriteString("- makes sense to someone who never saw the long video: no \"as we saw\", no unexplained term from earlier;\n")
	b.WriteString("- grabs in its first sentence: a surprising claim, a question, a concrete scene, real stakes;\n")
	b.WriteString("- lands a complete point by its last sentence.\n\n")
	fmt.Fprintf(&b, "Moments must not overlap and should sit at least %.0f seconds apart. Prefer the strongest over "+
		"covering the whole video. Score each 0–100 by how well it would do as a Short.\n\n", l.MinGap().Seconds())
	fmt.Fprintf(&b, "## The video\n\n**%s**\n\n", st.Idea().Title)
	if m := strings.TrimSpace(string(files[metadataFile])); m != "" {
		b.WriteString("Metadata:\n\n```json\n" + m + "\n```\n\n")
	}
	if sc := strings.TrimSpace(string(files[scriptFile])); sc != "" {
		b.WriteString("Script (scenes and visual notes):\n\n" + sc + "\n\n")
	}
	b.WriteString("## Narration, numbered, with times in seconds\n\n")
	for i, x := range sentences {
		fmt.Fprintf(&b, "%d. [%.1f–%.1f] %s\n", i+1, x.Start.Seconds(), x.End.Seconds(), x.Text)
	}
	return b.String()
}

// parseMoments turns the agent's answer into moment drafts; a moment
// whose sentence numbers do not fit the narration is dropped.
func parseMoments(raw json.RawMessage, sentences []Sentence) ([]storydomain.MomentDraft, error) {
	var out struct {
		Moments []struct {
			First  int    `json:"first"`
			Last   int    `json:"last"`
			Hook   string `json:"hook"`
			Reason string `json:"reason"`
			Score  int    `json:"score"`
		} `json:"moments"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("moments: decode: %w", err)
	}
	var drafts []storydomain.MomentDraft
	for _, m := range out.Moments {
		if m.First < 1 || m.Last < m.First || m.Last > len(sentences) {
			continue
		}
		c, err := storydomain.NewClip(sentences[m.First-1].Start, sentences[m.Last-1].End)
		if err != nil {
			continue
		}
		drafts = append(drafts, storydomain.MomentDraft{
			Clip: c, Hook: cut(m.Hook, storydomain.MaxHookLength), Reason: cut(m.Reason, storydomain.MaxReasonLength),
			Score: min(max(m.Score, 0), 100),
		})
	}
	return drafts, nil
}

// cut shortens s to at most n runes.
func cut(s string, n int) string {
	if r := []rune(strings.TrimSpace(s)); len(r) > n {
		return strings.TrimSpace(string(r[:n]))
	}
	return strings.TrimSpace(s)
}
