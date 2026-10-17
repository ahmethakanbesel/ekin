package story

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	"kurgu/pkg/timeutil"
)

// Shorts are vertical videos cut from a long video of the same story
// (docs/shorts.md): a moment of its narration, re-composed for 9:16.

// MaxShortsPerVideo is the ceiling for the per-video limit and for a
// pipeline's shorts setting.
const MaxShortsPerVideo = pipelinedomain.MaxShortsPerVideo

// MaxMomentsPerSource bounds the moments one long video keeps.
const MaxMomentsPerSource = 40

// Moment text limits.
const (
	MaxHookLength   = 120
	MaxReasonLength = 1000
)

// ShortsLimits bound which long videos may be cut and into what.
type ShortsLimits struct {
	sourceMin, sourceMax time.Duration
	clipMin, clipMax     time.Duration
	maxPerVideo          int
	minGap               time.Duration
}

// NewShortsLimits checks the limits: each minimum within its maximum,
// shorts within YouTube's bounds and short enough for the shortest
// source, at most MaxShortsPerVideo per video (0 turns shorts off), and
// a gap that is not negative.
func NewShortsLimits(sourceMin, sourceMax, clipMin, clipMax time.Duration, maxPerVideo int, minGap time.Duration) (ShortsLimits, error) {
	switch {
	case sourceMin <= 0 || sourceMin > sourceMax:
		return ShortsLimits{}, apperror.New(apperror.ValidationFailed, "the source minimum must be positive and not above the source maximum")
	case clipMin < pipelinedomain.MinShortDuration || clipMax > pipelinedomain.MaxShortDuration:
		return ShortsLimits{}, apperror.New(apperror.ValidationFailed, "shorts must last between 10 seconds and 3 minutes")
	case clipMin > clipMax:
		return ShortsLimits{}, apperror.New(apperror.ValidationFailed, "the short minimum must not be above the short maximum")
	case clipMin > sourceMin:
		return ShortsLimits{}, apperror.New(apperror.ValidationFailed, "the short minimum must fit in the source minimum")
	case maxPerVideo < 0 || maxPerVideo > MaxShortsPerVideo:
		return ShortsLimits{}, apperror.New(apperror.ValidationFailed, fmt.Sprintf("shorts per video must be between 0 and %d", MaxShortsPerVideo))
	case minGap < 0:
		return ShortsLimits{}, apperror.New(apperror.ValidationFailed, "the gap between shorts must not be negative")
	}
	return ShortsLimits{sourceMin: sourceMin, sourceMax: sourceMax, clipMin: clipMin, clipMax: clipMax, maxPerVideo: maxPerVideo, minGap: minGap}, nil
}

// DefaultShortsLimits are the limits without configuration.
func DefaultShortsLimits() ShortsLimits {
	return ShortsLimits{
		sourceMin: 4 * time.Minute, sourceMax: time.Hour, clipMin: 20 * time.Second, clipMax: time.Minute,
		maxPerVideo: 5, minGap: 30 * time.Second,
	}
}

func (l ShortsLimits) SourceMin() time.Duration { return l.sourceMin }
func (l ShortsLimits) SourceMax() time.Duration { return l.sourceMax }
func (l ShortsLimits) ClipMin() time.Duration   { return l.clipMin }
func (l ShortsLimits) ClipMax() time.Duration   { return l.clipMax }
func (l ShortsLimits) MaxPerVideo() int         { return l.maxPerVideo }
func (l ShortsLimits) MinGap() time.Duration    { return l.minGap }

// Clip is a span of a long video's narration, in seconds of its audio.
type Clip struct {
	start, end time.Duration
}

// NewClip returns the span from start to end, at millisecond precision.
func NewClip(start, end time.Duration) (Clip, error) {
	start, end = start.Truncate(time.Millisecond), end.Truncate(time.Millisecond)
	if start < 0 || end <= start {
		return Clip{}, apperror.New(apperror.ValidationFailed, "a clip must start at or after 0 and end after it starts")
	}
	return Clip{start: start, end: end}, nil
}

func (c Clip) Start() time.Duration  { return c.start }
func (c Clip) End() time.Duration    { return c.end }
func (c Clip) Length() time.Duration { return c.end - c.start }

// apart reports whether c and o are at least gap apart.
func (c Clip) apart(o Clip, gap time.Duration) bool {
	return o.start-c.end >= gap || c.start-o.end >= gap
}

// Segment is one sentence of a long video's narration, as timed in its
// narration/timings.json.
type Segment struct {
	Start, End time.Duration
}

// snap moves c onto sentence boundaries: its start to the nearest
// sentence start, its end to the nearest sentence end after that, so a
// short never begins or stops mid-sentence. The narration pauses
// between sentences, which makes those clean cuts.
func snap(c Clip, segments []Segment) (Clip, error) {
	segs := slices.Clone(segments)
	slices.SortFunc(segs, func(a, b Segment) int { return cmp.Compare(a.Start, b.Start) })
	nearest := func(t time.Duration, at func(Segment) time.Duration, ok func(Segment) bool) (time.Duration, bool) {
		var best time.Duration
		found := false
		for _, s := range segs {
			if !ok(s) {
				continue
			}
			if !found || absDuration(at(s)-t) < absDuration(best-t) {
				best, found = at(s), true
			}
		}
		return best, found
	}
	start, ok := nearest(c.start, func(s Segment) time.Duration { return s.Start }, func(s Segment) bool { return s.End > s.Start })
	if !ok {
		return Clip{}, apperror.New(apperror.ValidationFailed, "the long video's narration has no sentences")
	}
	end, _ := nearest(c.end, func(s Segment) time.Duration { return s.End }, func(s Segment) bool { return s.End > start })
	return NewClip(start, end)
}

// fit snaps c to sentences and keeps it within the short limits,
// ending it earlier, at a sentence end, when it runs too long.
func fit(c Clip, segments []Segment, l ShortsLimits) (Clip, error) {
	c, err := snap(c, segments)
	if err != nil {
		return Clip{}, err
	}
	if c.Length() > l.clipMax {
		best := time.Duration(0)
		for _, s := range segments {
			if s.End > c.start && s.End-c.start <= l.clipMax && s.End > best {
				best = s.End
			}
		}
		if best == 0 {
			return Clip{}, apperror.New(apperror.ValidationFailed, "the moment's first sentence is longer than a short may be")
		}
		c.end = best
	}
	if c.Length() < l.clipMin {
		return Clip{}, apperror.New(apperror.ValidationFailed, fmt.Sprintf("a short must last at least %s", l.clipMin))
	}
	return c, nil
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// MomentOrigin says who found a moment.
type MomentOrigin string

const (
	MomentByAgent  MomentOrigin = "agent"
	MomentByPerson MomentOrigin = "manual"
)

// MomentStatus is where a moment is.
//
//	suggested → made (a short was made from it) · discarded
type MomentStatus string

const (
	MomentSuggested MomentStatus = "suggested"
	MomentMade      MomentStatus = "made"
	MomentDiscarded MomentStatus = "discarded"
)

// MomentDraft is a moment as found, before it is snapped and checked.
type MomentDraft struct {
	Clip   Clip
	Hook   string // the short's working title
	Reason string // why the moment stands alone
	Score  int    // 0–100, higher is better
}

func (m MomentDraft) check() (MomentDraft, error) {
	m.Hook = strings.Join(strings.Fields(m.Hook), " ")
	m.Reason = strings.TrimSpace(m.Reason)
	switch {
	case utf8.RuneCountInString(m.Hook) > MaxHookLength:
		return m, apperror.New(apperror.ValidationFailed, "a moment's hook is too long")
	case utf8.RuneCountInString(m.Reason) > MaxReasonLength:
		return m, apperror.New(apperror.ValidationFailed, "a moment's reason is too long")
	case m.Score < 0 || m.Score > 100:
		return m, apperror.New(apperror.ValidationFailed, "a moment's score must be between 0 and 100")
	}
	return m, nil
}

// Moment is a span of a long video that could stand alone as a short.
type Moment struct {
	id        uuid.UUID
	sourceID  uuid.UUID
	clip      Clip
	hook      string
	reason    string
	score     int
	origin    MomentOrigin
	status    MomentStatus
	shortID   uuid.UUID // the short made from it
	createdAt time.Time
	updatedAt time.Time
}

func (m *Moment) ID() uuid.UUID            { return m.id }
func (m *Moment) SourceID() uuid.UUID      { return m.sourceID }
func (m *Moment) Clip() Clip               { return m.clip }
func (m *Moment) Hook() string             { return m.hook }
func (m *Moment) Reason() string           { return m.reason }
func (m *Moment) Score() int               { return m.score }
func (m *Moment) Origin() MomentOrigin     { return m.origin }
func (m *Moment) Status() MomentStatus     { return m.status }
func (m *Moment) ShortID() uuid.UUID       { return m.shortID }
func (m *Moment) CreatedAt() time.Time     { return m.createdAt }
func (m *Moment) UpdatedAt() time.Time     { return m.updatedAt }
func (m *Moment) suggested() bool          { return m.status == MomentSuggested }
func (m *Moment) touch(now time.Time)      { m.updatedAt = timeutil.Normalize(now) }
func (m *Moment) made() bool               { return m.status == MomentMade }
func (m *Moment) of(source uuid.UUID) bool { return m.sourceID == source }

// Moments returns the moments found in the long video source, in the
// order they come in it.
func (s *Story) Moments(source uuid.UUID) []*Moment {
	var out []*Moment
	for _, m := range s.moments {
		if m.of(source) {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b *Moment) int { return cmp.Compare(a.clip.start, b.clip.start) })
	return out
}

// Moment returns the moment with id.
func (s *Story) Moment(id uuid.UUID) (*Moment, error) {
	for _, m := range s.moments {
		if m.id == id {
			return m, nil
		}
	}
	return nil, apperror.New(apperror.NotFound, "moment not found")
}

// CanCut returns the long video source if shorts may be cut from it:
// a long video that is live (ready or published) and whose rendered
// length is within the limits.
func (s *Story) CanCut(source uuid.UUID, l ShortsLimits) (*Deliverable, error) {
	d, err := s.Deliverable(source)
	if err != nil {
		return nil, err
	}
	if !d.IsVideo() || d.video.Format() != pipelinedomain.FormatLong || d.clip != nil {
		return nil, apperror.New(apperror.ValidationFailed, "shorts are cut from long videos only")
	}
	if d.status != StatusReady && d.status != StatusPublished {
		return nil, apperror.New(apperror.InvalidTransition, "shorts are cut once the long video is approved and live; it is "+string(d.status))
	}
	length := d.measures.Duration
	if length == 0 {
		return nil, apperror.New(apperror.InvalidTransition, "the long video's length is not measured yet")
	}
	if length < l.sourceMin || length > l.sourceMax {
		return nil, apperror.New(apperror.ValidationFailed,
			fmt.Sprintf("shorts are cut from videos between %s and %s long; this one is %s", l.sourceMin, l.sourceMax, length.Truncate(time.Second)))
	}
	return d, nil
}

// SuggestMoments replaces the agent's earlier suggestions for source
// with drafts: each snapped to sentences and fitted to the short limits
// (those that cannot be are dropped), then taken best first, skipping
// any that come closer than the gap to one taken or already made.
// segments are the source's narration sentences. It returns the
// moments kept, best first.
func (s *Story) SuggestMoments(source uuid.UUID, drafts []MomentDraft, segments []Segment, l ShortsLimits, ids func() uuid.UUID, now time.Time) ([]*Moment, error) {
	if _, err := s.CanCut(source, l); err != nil {
		return nil, err
	}
	now = timeutil.Normalize(now)
	s.moments = slices.DeleteFunc(s.moments, func(m *Moment) bool {
		return m.of(source) && m.origin == MomentByAgent && m.suggested()
	})
	var fitted []MomentDraft
	for _, d := range drafts {
		d, err := d.check()
		if err != nil {
			continue
		}
		if d.Clip, err = fit(d.Clip, segments, l); err != nil {
			continue
		}
		fitted = append(fitted, d)
	}
	slices.SortStableFunc(fitted, func(a, b MomentDraft) int { return cmp.Compare(b.Score, a.Score) })
	var kept []*Moment
	room := MaxMomentsPerSource - len(s.Moments(source))
	for _, d := range fitted {
		if len(kept) == room {
			break
		}
		if !s.clear(source, d.Clip, l.minGap, kept) {
			continue
		}
		m := &Moment{
			id: ids(), sourceID: source, clip: d.Clip, hook: d.Hook, reason: d.Reason, score: d.Score,
			origin: MomentByAgent, status: MomentSuggested, createdAt: now, updatedAt: now,
		}
		kept = append(kept, m)
	}
	s.moments = append(s.moments, kept...)
	s.touch(now)
	return kept, nil
}

// clear reports whether c is at least gap away from the moments made
// from source and from also.
func (s *Story) clear(source uuid.UUID, c Clip, gap time.Duration, also []*Moment) bool {
	for _, m := range s.moments {
		if m.of(source) && m.made() && !c.apart(m.clip, gap) {
			return false
		}
	}
	for _, m := range also {
		if !c.apart(m.clip, gap) {
			return false
		}
	}
	return true
}

// AddMoment adds a moment a person chose in source, snapped and fitted
// like the agent's.
func (s *Story) AddMoment(source uuid.UUID, d MomentDraft, segments []Segment, l ShortsLimits, ids func() uuid.UUID, now time.Time) (*Moment, error) {
	if _, err := s.CanCut(source, l); err != nil {
		return nil, err
	}
	if len(s.Moments(source)) >= MaxMomentsPerSource {
		return nil, apperror.New(apperror.ValidationFailed, fmt.Sprintf("a long video keeps at most %d moments; discard some first", MaxMomentsPerSource))
	}
	d, err := d.check()
	if err != nil {
		return nil, err
	}
	if d.Clip, err = fit(d.Clip, segments, l); err != nil {
		return nil, err
	}
	now = timeutil.Normalize(now)
	m := &Moment{
		id: ids(), sourceID: source, clip: d.Clip, hook: d.Hook, reason: d.Reason,
		origin: MomentByPerson, status: MomentSuggested, createdAt: now, updatedAt: now,
	}
	s.moments = append(s.moments, m)
	s.touch(now)
	return m, nil
}

// AdjustMoment moves a suggested moment to clip (snapped and fitted)
// and, when hook is not nil, renames it. A person's adjustment keeps
// the moment through the agent's next suggestions.
func (s *Story) AdjustMoment(id uuid.UUID, clip *Clip, hook *string, segments []Segment, l ShortsLimits, now time.Time) (*Moment, error) {
	m, err := s.suggestedMoment(id)
	if err != nil {
		return nil, err
	}
	d := MomentDraft{Clip: m.clip, Hook: m.hook, Reason: m.reason, Score: m.score}
	if hook != nil {
		d.Hook = *hook
	}
	if d, err = d.check(); err != nil {
		return nil, err
	}
	if clip != nil {
		if d.Clip, err = fit(*clip, segments, l); err != nil {
			return nil, err
		}
	}
	m.clip, m.hook, m.origin = d.Clip, d.Hook, MomentByPerson
	m.touch(now)
	s.touch(now)
	return m, nil
}

// DiscardMoment sets a suggested moment aside.
func (s *Story) DiscardMoment(id uuid.UUID, now time.Time) error {
	m, err := s.suggestedMoment(id)
	if err != nil {
		return err
	}
	m.status = MomentDiscarded
	m.touch(now)
	s.touch(now)
	return nil
}

func (s *Story) suggestedMoment(id uuid.UUID) (*Moment, error) {
	m, err := s.Moment(id)
	if err != nil {
		return nil, err
	}
	if !m.suggested() {
		return nil, apperror.New(apperror.InvalidTransition, "the moment is "+string(m.status))
	}
	return m, nil
}

// Shorts returns the shorts cut from source.
func (s *Story) Shorts(source uuid.UUID) []*Deliverable {
	var out []*Deliverable
	for _, d := range s.deliverables {
		if d.sourceID == source && d.clip != nil {
			out = append(out, d)
		}
	}
	return out
}

// AddShorts makes one vertical short per moment of source, queued for
// drafting, and marks the moments made. The moments must be suggested,
// of source, and at least the gap apart from each other and from the
// shorts already made; source may have at most the limit's shorts.
func (s *Story) AddShorts(source uuid.UUID, momentIDs []uuid.UUID, l ShortsLimits, ids func() uuid.UUID, now time.Time) ([]*Deliverable, error) {
	src, err := s.CanCut(source, l)
	if err != nil {
		return nil, err
	}
	if len(momentIDs) == 0 {
		return nil, apperror.New(apperror.ValidationFailed, "choose at least one moment")
	}
	if have := len(s.Shorts(source)); have+len(momentIDs) > l.maxPerVideo {
		return nil, apperror.New(apperror.ValidationFailed,
			fmt.Sprintf("a long video may have at most %d shorts; it has %d", l.maxPerVideo, have))
	}
	var chosen []*Moment
	for _, id := range momentIDs {
		m, err := s.suggestedMoment(id)
		if err != nil {
			return nil, err
		}
		if !m.of(source) {
			return nil, apperror.New(apperror.ValidationFailed, "the moment is from another video")
		}
		if slices.Contains(chosen, m) {
			return nil, apperror.New(apperror.ValidationFailed, "a moment is chosen twice")
		}
		if m.clip.Length() < l.clipMin || m.clip.Length() > l.clipMax {
			return nil, apperror.New(apperror.ValidationFailed,
				fmt.Sprintf("shorts must last between %s and %s", l.clipMin, l.clipMax))
		}
		if !s.clear(source, m.clip, l.minGap, chosen) {
			return nil, apperror.New(apperror.ValidationFailed,
				fmt.Sprintf("shorts of one video must be at least %s apart", l.minGap))
		}
		chosen = append(chosen, m)
	}
	now = timeutil.Normalize(now)
	orientation := pipelinedomain.Vertical
	var made []*Deliverable
	for _, m := range chosen {
		spec, err := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, orientation, m.clip.Length().Round(time.Second))
		if err != nil {
			return nil, err
		}
		clip := m.clip
		d := &Deliverable{
			id: ids(), kind: pipelinedomain.KindVideo, role: RoleDerived, sourceID: src.id, language: src.language,
			video: &spec, clip: &clip, status: StatusQueued, createdAt: now, updatedAt: now,
		}
		s.deliverables = append(s.deliverables, d)
		m.status, m.shortID = MomentMade, d.id
		m.touch(now)
		made = append(made, d)
	}
	s.touch(now)
	return made, nil
}

// Recut moves the clip of a short being drafted (the agent moved its
// cut by whole sentences): the new clip must keep within the short
// limits and the gap to the other shorts of its long video. The
// short's length and the moment it was made from follow.
func (s *Story) Recut(id uuid.UUID, clip Clip, l ShortsLimits, now time.Time) error {
	d, err := s.find(id, StatusDrafting)
	if err != nil {
		return err
	}
	if d.clip == nil {
		return apperror.New(apperror.ValidationFailed, "only a short cut from a long video has a clip")
	}
	if clip.Length() < l.clipMin || clip.Length() > l.clipMax {
		return apperror.New(apperror.ValidationFailed, fmt.Sprintf("shorts must last between %s and %s", l.clipMin, l.clipMax))
	}
	for _, m := range s.moments {
		if m.of(d.sourceID) && m.made() && m.shortID != d.id && !clip.apart(m.clip, l.minGap) {
			return apperror.New(apperror.ValidationFailed,
				fmt.Sprintf("the new cut comes closer than %s to another short of the long video", l.minGap))
		}
	}
	spec, err := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, d.video.Orientation(), clip.Length().Round(time.Second))
	if err != nil {
		return err
	}
	now = timeutil.Normalize(now)
	d.clip, d.video = &clip, &spec
	d.updatedAt = now
	for _, m := range s.moments {
		if m.shortID == d.id {
			m.clip = clip
			m.touch(now)
		}
	}
	s.touch(now)
	return nil
}
