package story

import (
	"maps"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	"kurgu/pkg/timeutil"
)

// Role is how a deliverable relates to the story.
type Role string

const (
	// RoleLead is produced first, from the brief.
	RoleLead Role = "lead"
	// RoleDerived is built from the approved lead (the article around a
	// video, or a short cut from an article).
	RoleDerived Role = "derived"
	// RoleTranslation is an approved article in another language.
	RoleTranslation Role = "translation"
)

// Status is where a deliverable is.
//
//	waiting → queued → drafting → rendering → review ⇄ (changes) queued
//	review → finalizing → ready → publishing → published
//	review → rejected; drafting|rendering|finalizing|publishing → failed → (retry)
type Status string

const (
	StatusWaiting    Status = "waiting"    // derived: waits for the lead's approval
	StatusQueued     Status = "queued"     // the agent will draft (or revise)
	StatusDrafting   Status = "drafting"   // the agent is writing
	StatusRendering  Status = "rendering"  // video: preview render
	StatusReview     Status = "review"     // waiting for a human
	StatusFinalizing Status = "finalizing" // video: full-resolution render
	StatusReady      Status = "ready"      // approved, final assets made
	StatusPublishing Status = "publishing"
	StatusPublished  Status = "published"
	StatusRejected   Status = "rejected"
	StatusFailed     Status = "failed"
)

// ParseStatus parses a status.
func ParseStatus(s string) (Status, error) {
	switch st := Status(s); st {
	case StatusWaiting, StatusQueued, StatusDrafting, StatusRendering, StatusReview, StatusFinalizing,
		StatusReady, StatusPublishing, StatusPublished, StatusRejected, StatusFailed:
		return st, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown deliverable status")
}

// Scope narrows feedback to one part of a deliverable.
type Scope string

const (
	ScopeAll      Scope = "all"
	ScopeScript   Scope = "script"
	ScopeVoice    Scope = "voice"
	ScopeVisuals  Scope = "visuals"
	ScopeMusic    Scope = "music"
	ScopeMetadata Scope = "metadata"
)

// Feedback is a reviewer's change request.
type Feedback struct {
	Text      string    `json:"text"`
	Scope     Scope     `json:"scope"`
	AtSeconds *float64  `json:"atSeconds,omitempty"` // video position it refers to
	By        uuid.UUID `json:"by"`
	At        time.Time `json:"at"`
}

// NewFeedback checks a change request.
func NewFeedback(text string, scope Scope, atSeconds *float64, by uuid.UUID, now time.Time) (Feedback, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Feedback{}, apperror.New(apperror.ValidationFailed, "feedback is required")
	}
	if utf8.RuneCountInString(text) > MaxFeedbackRunes {
		return Feedback{}, apperror.New(apperror.ValidationFailed, "feedback is too long")
	}
	switch scope {
	case "":
		scope = ScopeAll
	case ScopeAll, ScopeScript, ScopeVoice, ScopeVisuals, ScopeMusic, ScopeMetadata:
	default:
		return Feedback{}, apperror.New(apperror.ValidationFailed, "unknown feedback scope")
	}
	if atSeconds != nil && *atSeconds < 0 {
		return Feedback{}, apperror.New(apperror.ValidationFailed, "feedback position must not be negative")
	}
	return Feedback{Text: text, Scope: scope, AtSeconds: atSeconds, By: by, At: timeutil.Normalize(now)}, nil
}

// Publication is where a deliverable went live.
type Publication struct {
	URL        string    `json:"url"`
	ExternalID string    `json:"externalId"`
	At         time.Time `json:"at"`
}

// Deliverable is one output of a story.
type Deliverable struct {
	id         uuid.UUID
	kind       pipelinedomain.Kind
	role       Role
	sourceID   uuid.UUID
	language   pipelinedomain.Language
	video      *pipelinedomain.VideoSpec
	article    *pipelinedomain.ArticleSpec
	status     Status
	revision   int
	pending    *Feedback
	failure    string
	failedStep Status
	session    string
	approvedBy uuid.UUID
	approvedAt time.Time
	published  *Publication
	createdAt  time.Time
	updatedAt  time.Time
}

func (d *Deliverable) ID() uuid.UUID                        { return d.id }
func (d *Deliverable) Kind() pipelinedomain.Kind            { return d.kind }
func (d *Deliverable) Role() Role                           { return d.role }
func (d *Deliverable) SourceID() uuid.UUID                  { return d.sourceID }
func (d *Deliverable) Language() pipelinedomain.Language    { return d.language }
func (d *Deliverable) Video() *pipelinedomain.VideoSpec     { return d.video }
func (d *Deliverable) Article() *pipelinedomain.ArticleSpec { return d.article }
func (d *Deliverable) Status() Status                       { return d.status }
func (d *Deliverable) Revision() int                        { return d.revision }
func (d *Deliverable) PendingFeedback() *Feedback           { return d.pending }
func (d *Deliverable) Failure() string                      { return d.failure }
func (d *Deliverable) FailedStep() Status                   { return d.failedStep }
func (d *Deliverable) Session() string                      { return d.session }
func (d *Deliverable) ApprovedBy() uuid.UUID                { return d.approvedBy }
func (d *Deliverable) ApprovedAt() time.Time                { return d.approvedAt }
func (d *Deliverable) Publication() *Publication            { return d.published }
func (d *Deliverable) CreatedAt() time.Time                 { return d.createdAt }
func (d *Deliverable) UpdatedAt() time.Time                 { return d.updatedAt }
func (d *Deliverable) IsVideo() bool                        { return d.kind == pipelinedomain.KindVideo }

// Revision is one agent pass over a deliverable: what was asked (the
// feedback, if any) and what came out (artifact blob keys by name).
type Revision struct {
	DeliverableID uuid.UUID
	Number        int
	Feedback      *Feedback
	Artifacts     map[string]string
	Notes         string
	CreatedAt     time.Time
}

// Artifact names shared by the production pipeline.
const (
	ArtifactWorkdir   = "workdir.tar.gz" // the agent's working directory
	ArtifactPreview   = "preview.mp4"
	ArtifactFinal     = "final.mp4"
	ArtifactPoster    = "poster.jpg"
	ArtifactArticle   = "article.md"
	ArtifactMetadata  = "metadata.json"
	ArtifactScript    = "script.md"
	ArtifactNarration = "narration.wav"
)

func (s *Story) find(id uuid.UUID, allowed ...Status) (*Deliverable, error) {
	d, err := s.Deliverable(id)
	if err != nil {
		return nil, err
	}
	for _, st := range allowed {
		if d.status == st {
			return d, nil
		}
	}
	return nil, apperror.New(apperror.InvalidTransition, "the "+string(d.kind)+" is "+string(d.status))
}

func (d *Deliverable) set(st Status, now time.Time) {
	d.status = st
	d.updatedAt = timeutil.Normalize(now)
}

// StartDrafting hands a queued deliverable to the agent and returns the
// feedback to address (nil for a first draft).
func (s *Story) StartDrafting(id uuid.UUID, now time.Time) (*Feedback, error) {
	d, err := s.find(id, StatusQueued)
	if err != nil {
		return nil, err
	}
	d.set(StatusDrafting, now)
	s.touch(now)
	return d.pending, nil
}

// Drafted records the agent's output as the next revision. Videos go on
// to the preview render; articles straight to review.
func (s *Story) Drafted(id uuid.UUID, artifacts map[string]string, session, notes string, now time.Time) (Revision, error) {
	d, err := s.find(id, StatusDrafting)
	if err != nil {
		return Revision{}, err
	}
	d.revision++
	rev := Revision{
		DeliverableID: d.id, Number: d.revision, Feedback: d.pending,
		Artifacts: maps.Clone(artifacts), Notes: notes, CreatedAt: timeutil.Normalize(now),
	}
	d.pending, d.failure, d.failedStep = nil, "", ""
	if session != "" {
		d.session = session
	}
	if d.IsVideo() {
		d.set(StatusRendering, now)
	} else {
		d.set(StatusReview, now)
	}
	s.touch(now)
	return rev, nil
}

// Rendered records a finished render: a preview puts the video in
// review, a final render makes it ready.
func (s *Story) Rendered(id uuid.UUID, now time.Time) error {
	d, err := s.find(id, StatusRendering, StatusFinalizing)
	if err != nil {
		return err
	}
	if d.status == StatusRendering {
		d.set(StatusReview, now)
	} else {
		d.set(StatusReady, now)
	}
	s.touch(now)
	return nil
}

// RequestChanges sends a deliverable in review back to the agent.
func (s *Story) RequestChanges(id uuid.UUID, fb Feedback, now time.Time) error {
	d, err := s.find(id, StatusReview)
	if err != nil {
		return err
	}
	d.pending = &fb
	d.set(StatusQueued, now)
	s.touch(now)
	return nil
}

// Approve accepts a deliverable in review. Videos need a final render;
// articles are ready at once. Approving an article adds its
// translations. It returns the deliverables that became queued. Derived
// deliverables are released separately (ReleaseDerived), once the lead
// is live and they can link to it.
func (s *Story) Approve(id, by uuid.UUID, newID func() uuid.UUID, now time.Time) ([]*Deliverable, error) {
	d, err := s.find(id, StatusReview)
	if err != nil {
		return nil, err
	}
	d.approvedBy, d.approvedAt = by, timeutil.Normalize(now)
	if d.IsVideo() {
		d.set(StatusFinalizing, now)
	} else {
		d.set(StatusReady, now)
	}
	var queued []*Deliverable
	if d.article != nil && d.role != RoleTranslation {
		for _, lang := range d.article.Translations() {
			if lang == d.language || s.hasTranslation(d.id, lang) {
				continue
			}
			t := &Deliverable{
				id: newID(), kind: pipelinedomain.KindArticle, role: RoleTranslation, sourceID: d.id,
				language: lang, article: d.article, status: StatusQueued,
				createdAt: timeutil.Normalize(now), updatedAt: timeutil.Normalize(now),
			}
			s.deliverables = append(s.deliverables, t)
			queued = append(queued, t)
		}
	}
	s.touch(now)
	return queued, nil
}

// ReleaseDerived queues the deliverables waiting on source, which must
// be ready or published, and returns them.
func (s *Story) ReleaseDerived(source uuid.UUID, now time.Time) ([]*Deliverable, error) {
	if _, err := s.find(source, StatusReady, StatusPublished); err != nil {
		return nil, err
	}
	var queued []*Deliverable
	for _, other := range s.deliverables {
		if other.sourceID == source && other.status == StatusWaiting {
			other.set(StatusQueued, now)
			queued = append(queued, other)
		}
	}
	if len(queued) > 0 {
		s.touch(now)
	}
	return queued, nil
}

func (s *Story) hasTranslation(source uuid.UUID, lang pipelinedomain.Language) bool {
	for _, d := range s.deliverables {
		if d.role == RoleTranslation && d.sourceID == source && d.language == lang {
			return true
		}
	}
	return false
}

// Reject drops a deliverable, and whatever was waiting on it.
func (s *Story) Reject(id uuid.UUID, now time.Time) error {
	d, err := s.find(id, StatusReview, StatusFailed, StatusQueued, StatusWaiting)
	if err != nil {
		return err
	}
	d.set(StatusRejected, now)
	for _, other := range s.deliverables {
		if other.sourceID == d.id && other.status == StatusWaiting {
			other.set(StatusRejected, now)
		}
	}
	s.touch(now)
	return nil
}

// Fail records that the current step failed.
func (s *Story) Fail(id uuid.UUID, reason string, now time.Time) error {
	d, err := s.find(id, StatusDrafting, StatusRendering, StatusFinalizing, StatusPublishing)
	if err != nil {
		return err
	}
	if len(reason) > 4000 {
		reason = reason[:4000]
	}
	d.failedStep, d.failure = d.status, reason
	d.set(StatusFailed, now)
	s.touch(now)
	return nil
}

// Retry resumes a failed deliverable at the step that failed and returns
// the status it resumed in.
func (s *Story) Retry(id uuid.UUID, now time.Time) (Status, error) {
	d, err := s.find(id, StatusFailed)
	if err != nil {
		return "", err
	}
	next := d.failedStep
	switch next {
	case StatusDrafting:
		next = StatusQueued
	case StatusPublishing:
		next = StatusReady
	case StatusRendering, StatusFinalizing:
	default:
		next = StatusQueued
	}
	d.failure, d.failedStep = "", ""
	d.set(next, now)
	s.touch(now)
	return next, nil
}

// StartPublishing marks a ready deliverable as going out.
func (s *Story) StartPublishing(id uuid.UUID, now time.Time) error {
	d, err := s.find(id, StatusReady)
	if err != nil {
		return err
	}
	d.set(StatusPublishing, now)
	s.touch(now)
	return nil
}

// Published records where the deliverable went live.
func (s *Story) Published(id uuid.UUID, pub Publication, now time.Time) error {
	d, err := s.find(id, StatusPublishing)
	if err != nil {
		return err
	}
	if pub.URL == "" {
		return apperror.New(apperror.ValidationFailed, "publication URL is required")
	}
	pub.At = timeutil.Normalize(now)
	d.published = &pub
	d.set(StatusPublished, now)
	s.touch(now)
	return nil
}
