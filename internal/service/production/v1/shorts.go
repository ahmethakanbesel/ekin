package production

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// Sentence is one sentence of a long video's narration, as spoken.
type Sentence struct {
	Text       string
	Start, End time.Duration
}

// Moments is what the moments panel of a long video shows.
type Moments struct {
	Story *storydomain.Story
	// Source is the long video.
	Source *storydomain.Deliverable
	// Blocked says why shorts cannot be cut from it (yet); "" when they
	// can. Sentences are only read when they can.
	Blocked   string
	Sentences []Sentence
	Limits    storydomain.ShortsLimits
}

// Moments returns the moments found in a long video, with its
// narration's sentences so a person can choose more.
func (s *Service) Moments(ctx context.Context, workspaceID, storyID, sourceID uuid.UUID) (Moments, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return Moments{}, err
	}
	src, err := st.Deliverable(sourceID)
	if err != nil {
		return Moments{}, err
	}
	out := Moments{Story: st, Source: src, Limits: s.cfg.Shorts}
	if _, err := st.CanCut(sourceID, s.cfg.Shorts); err != nil {
		if code, _ := apperror.GetCode(err); code == apperror.NotFound {
			return Moments{}, err
		}
		out.Blocked = apperror.Message(err)
		return out, nil
	}
	if out.Sentences, err = s.sentences(ctx, src); err != nil {
		return Moments{}, err
	}
	return out, nil
}

// AddMoment adds a moment a person chose in a long video.
func (s *Service) AddMoment(ctx context.Context, workspaceID, storyID, sourceID uuid.UUID, d storydomain.MomentDraft) (*storydomain.Story, *storydomain.Moment, error) {
	segs, err := s.segmentsOf(ctx, workspaceID, storyID, sourceID)
	if err != nil {
		return nil, nil, err
	}
	var m *storydomain.Moment
	st, err := s.change(ctx, workspaceID, storyID, func(_ context.Context, st *storydomain.Story) error {
		m, err = st.AddMoment(sourceID, d, segs, s.cfg.Shorts, id.New, s.now())
		return err
	})
	return st, m, err
}

// AdjustMoment moves or renames a suggested moment.
func (s *Service) AdjustMoment(ctx context.Context, workspaceID, storyID, momentID uuid.UUID, clip *storydomain.Clip, hook *string) (*storydomain.Story, *storydomain.Moment, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, nil, err
	}
	m, err := st.Moment(momentID)
	if err != nil {
		return nil, nil, err
	}
	var segs []storydomain.Segment
	if clip != nil {
		if segs, err = s.segmentsOf(ctx, workspaceID, storyID, m.SourceID()); err != nil {
			return nil, nil, err
		}
	}
	st, err = s.change(ctx, workspaceID, storyID, func(_ context.Context, st *storydomain.Story) error {
		m, err = st.AdjustMoment(momentID, clip, hook, segs, s.cfg.Shorts, s.now())
		return err
	})
	return st, m, err
}

// DiscardMoment sets a suggested moment aside.
func (s *Service) DiscardMoment(ctx context.Context, workspaceID, storyID, momentID uuid.UUID) (*storydomain.Story, error) {
	return s.change(ctx, workspaceID, storyID, func(_ context.Context, st *storydomain.Story) error {
		return st.DiscardMoment(momentID, s.now())
	})
}

// MakeShorts makes a short from each moment of a long video and queues
// their drafts.
func (s *Service) MakeShorts(ctx context.Context, workspaceID, storyID, sourceID uuid.UUID, momentIDs []uuid.UUID) (*storydomain.Story, error) {
	return s.change(ctx, workspaceID, storyID, func(ctx context.Context, st *storydomain.Story) error {
		made, err := st.AddShorts(sourceID, momentIDs, s.cfg.Shorts, id.New, s.now())
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
}

// segmentsOf reads the sentence timings of a long video shorts may be
// cut from.
func (s *Service) segmentsOf(ctx context.Context, workspaceID, storyID, sourceID uuid.UUID) ([]storydomain.Segment, error) {
	st, err := s.stories.Get(ctx, workspaceID, storyID)
	if err != nil {
		return nil, err
	}
	src, err := st.CanCut(sourceID, s.cfg.Shorts)
	if err != nil {
		return nil, err
	}
	sentences, err := s.sentences(ctx, src)
	if err != nil {
		return nil, err
	}
	return segments(sentences), nil
}

func segments(sentences []Sentence) []storydomain.Segment {
	out := make([]storydomain.Segment, 0, len(sentences))
	for _, x := range sentences {
		out = append(out, storydomain.Segment{Start: x.Start, End: x.End})
	}
	return out
}

// sentences reads the narration sentences of a video's latest revision
// from the timings `ekin tool tts` wrote.
func (s *Service) sentences(ctx context.Context, d *storydomain.Deliverable) ([]Sentence, error) {
	files, err := s.revisionFiles(ctx, d, narrationTimings)
	if err != nil {
		return nil, err
	}
	raw, ok := files[narrationTimings]
	if !ok {
		return nil, apperror.New(apperror.NotFound, "the long video kept no narration timings")
	}
	return parseSentences(raw)
}

// parseSentences reads timings.json's segments.
func parseSentences(raw []byte) ([]Sentence, error) {
	var t struct {
		Segments []struct {
			Text  string  `json:"text"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, apperror.Wrap(apperror.InternalError, err, "the long video's narration timings are unreadable")
	}
	out := make([]Sentence, 0, len(t.Segments))
	for _, seg := range t.Segments {
		if seg.End <= seg.Start || math.IsNaN(seg.Start) || math.IsInf(seg.End, 0) {
			continue
		}
		out = append(out, Sentence{Text: seg.Text, Start: seconds(seg.Start), End: seconds(seg.End)})
	}
	if len(out) == 0 {
		return nil, apperror.New(apperror.NotFound, "the long video's narration has no timed sentences")
	}
	return out, nil
}

func seconds(s float64) time.Duration {
	return time.Duration(math.Round(s*1000)) * time.Millisecond
}
