package story

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
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
//
// A video whose story reviews scripts first goes queued → drafting →
// review (the script) ⇄ (changes) queued, and approving the script
// queues it again to be built as a video (see Story.AtScript).
//
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

// Note is one thing a reviewer wants changed: what it is about, where
// in the video (nil for the whole draft), and what should change.
type Note struct {
	Text      string   `json:"text"`
	Scope     Scope    `json:"scope"`
	AtSeconds *float64 `json:"atSeconds,omitempty"` // video position it refers to
}

// Feedback is a reviewer's change request: the notes of one review
// round, sent together.
type Feedback struct {
	Notes []Note    `json:"notes"`
	By    uuid.UUID `json:"by"`
	At    time.Time `json:"at"`
}

// MaxFeedbackNotes is the most notes one change request carries.
const MaxFeedbackNotes = 20

// NewFeedback checks a change request. Notes are kept in reading order:
// those about the whole draft first, then the timed ones by time.
func NewFeedback(notes []Note, by uuid.UUID, now time.Time) (Feedback, error) {
	if len(notes) == 0 {
		return Feedback{}, apperror.New(apperror.ValidationFailed, "feedback is required")
	}
	if len(notes) > MaxFeedbackNotes {
		return Feedback{}, apperror.New(apperror.ValidationFailed, fmt.Sprintf("feedback has more than %d notes", MaxFeedbackNotes))
	}
	out := make([]Note, 0, len(notes))
	for _, n := range notes {
		checked, err := newNote(n)
		if err != nil {
			return Feedback{}, err
		}
		out = append(out, checked)
	}
	slices.SortStableFunc(out, func(a, b Note) int {
		switch {
		case a.AtSeconds == nil && b.AtSeconds == nil:
			return 0
		case a.AtSeconds == nil:
			return -1
		case b.AtSeconds == nil:
			return 1
		}
		return cmp.Compare(*a.AtSeconds, *b.AtSeconds)
	})
	return Feedback{Notes: out, By: by, At: timeutil.Normalize(now)}, nil
}

func newNote(n Note) (Note, error) {
	n.Text = strings.TrimSpace(n.Text)
	if n.Text == "" {
		return Note{}, apperror.New(apperror.ValidationFailed, "feedback is required")
	}
	if utf8.RuneCountInString(n.Text) > MaxFeedbackRunes {
		return Note{}, apperror.New(apperror.ValidationFailed, "feedback is too long")
	}
	switch n.Scope {
	case "":
		n.Scope = ScopeAll
	case ScopeAll, ScopeScript, ScopeVoice, ScopeVisuals, ScopeMusic, ScopeMetadata:
	default:
		return Note{}, apperror.New(apperror.ValidationFailed, "unknown feedback scope")
	}
	if n.AtSeconds != nil {
		if *n.AtSeconds < 0 {
			return Note{}, apperror.New(apperror.ValidationFailed, "feedback position must not be negative")
		}
		at := *n.AtSeconds
		n.AtSeconds = &at
	}
	return n, nil
}

// UnmarshalJSON reads feedback as stored, also from before it carried
// several notes ({"text", "scope", "atSeconds", "by", "at"}), which
// becomes one note.
func (f *Feedback) UnmarshalJSON(b []byte) error {
	var raw struct {
		Notes     []Note    `json:"notes"`
		Text      string    `json:"text"`
		Scope     Scope     `json:"scope"`
		AtSeconds *float64  `json:"atSeconds"`
		By        uuid.UUID `json:"by"`
		At        time.Time `json:"at"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	notes := raw.Notes
	if len(notes) == 0 && raw.Text != "" {
		scope := raw.Scope
		if scope == "" {
			scope = ScopeAll
		}
		notes = []Note{{Text: raw.Text, Scope: scope, AtSeconds: raw.AtSeconds}}
	}
	*f = Feedback{Notes: notes, By: raw.By, At: raw.At}
	return nil
}

// Measures are what a revision came out as, not what was asked for:
// the rendered video's length, the words spoken or written and the
// voice that spoke them. Zero means not measured (yet).
type Measures struct {
	Duration time.Duration
	Words    int
	Narrator Narrator
}

// Render is the scale (1 = 1080p) and frame rate a video was rendered
// at. Zero when it is not known.
type Render struct {
	Scale float64 `json:"scale"`
	FPS   int     `json:"fps"`
}

// Covers reports whether a video rendered at r can stand in for one
// rendered at want: at least as sharp and at the same frame rate. An
// unknown render covers nothing.
func (r Render) Covers(want Render) bool {
	return r.Scale > 0 && r.FPS > 0 && r.Scale >= want.Scale && r.FPS == want.FPS
}

// Narrator is the voice that read a video's narration: the TTS engine,
// its voice ("" for the engine's default) and, for ElevenLabs, the
// model. Zero when nothing was narrated or it is not known.
type Narrator struct {
	Engine string `json:"engine"`
	Voice  string `json:"voice,omitempty"`
	Model  string `json:"model,omitempty"`
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
	// publishJob is the job publishing the deliverable: only its retry
	// may resume a publication cut short.
	publishJob uuid.UUID
	measures   Measures
	// scriptApprovedAt is when a person approved the video's script;
	// zero until then, or when the story does not review scripts.
	scriptApprovedAt time.Time
	// clip is the span of the source long video a short was cut from;
	// nil for every other deliverable.
	clip      *Clip
	createdAt time.Time
	updatedAt time.Time
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
func (d *Deliverable) PublishJob() uuid.UUID                { return d.publishJob }
func (d *Deliverable) Measures() Measures                   { return d.measures }
func (d *Deliverable) ScriptApprovedAt() time.Time          { return d.scriptApprovedAt }
func (d *Deliverable) CreatedAt() time.Time                 { return d.createdAt }
func (d *Deliverable) UpdatedAt() time.Time                 { return d.updatedAt }
func (d *Deliverable) Clip() *Clip                          { return d.clip }
func (d *Deliverable) IsVideo() bool                        { return d.kind == pipelinedomain.KindVideo }

// Revision is one agent pass over a deliverable: what was asked (the
// feedback, if any) and what came out (artifact blob keys by name).
type Revision struct {
	DeliverableID uuid.UUID
	Number        int
	Feedback      *Feedback
	Artifacts     map[string]string
	Notes         string
	Measures      Measures
	// Preview is what the revision's preview was rendered at; zero
	// before it is rendered, and for revisions rendered before it was
	// recorded.
	Preview Render
	// Stage is what the revision holds: StageScript for a video's script
	// waiting for approval, StageFull otherwise.
	Stage Stage
	// EditedBy is the person who edited this revision in place; Nil when
	// the agent made it.
	EditedBy uuid.UUID
	// Annotation is a person's note on the revision; zero when it has
	// none.
	Annotation Annotation
	CreatedAt  time.Time
}

// MaxAnnotationRunes bounds a revision's note.
const MaxAnnotationRunes = 4000

// Annotation is the one note a person keeps on a revision, for the team:
// what was decided, what to remember. It is not feedback and never
// reaches the agent. Saving it again replaces it.
type Annotation struct {
	Text string
	By   uuid.UUID
	At   time.Time
}

// NewAnnotation is by's note at now. Blank text is the zero Annotation,
// which removes the note.
func NewAnnotation(text string, by uuid.UUID, now time.Time) (Annotation, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Annotation{}, nil
	}
	if n := utf8.RuneCountInString(text); n > MaxAnnotationRunes {
		return Annotation{}, apperror.New(apperror.ValidationFailed, fmt.Sprintf("a note holds at most %d characters, not %d", MaxAnnotationRunes, n))
	}
	if by == uuid.Nil {
		return Annotation{}, apperror.New(apperror.ValidationFailed, "a note needs its author")
	}
	return Annotation{Text: text, By: by, At: timeutil.Normalize(now)}, nil
}

// IsZero reports whether there is no note.
func (a Annotation) IsZero() bool { return a.Text == "" }

// Stage is how much of a deliverable a revision holds.
type Stage string

const (
	// StageFull is the whole deliverable: a rendered video or an article.
	StageFull Stage = ""
	// StageScript is a video's script and narration text alone, for a
	// person to approve before the video is built.
	StageScript Stage = "script"
)

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
	// ArtifactNarrationText is what the voice says (or will say, while a
	// script waits for approval).
	ArtifactNarrationText = "narration.txt"
	// ArtifactCaptionsSRT and ArtifactCaptionsVTT are the narration's
	// caption track, made on request from its word timings.
	ArtifactCaptionsSRT = "captions.srt"
	ArtifactCaptionsVTT = "captions.vtt"
)

// Captioned reports whether the revision has narration with word timings
// to make captions from.
func (r Revision) Captioned() bool {
	_, ok := r.Artifacts[ArtifactNarration]
	return ok
}

// ArtifactNames are the names of the revision's artifacts, sorted: the
// stored ones and, for narrated video, its captions.
func (r Revision) ArtifactNames() []string {
	names := slices.Collect(maps.Keys(r.Artifacts))
	if r.Captioned() {
		names = append(names, ArtifactCaptionsSRT, ArtifactCaptionsVTT)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

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

// AtScript reports whether deliverable id is a video whose story
// reviews scripts first and whose script is not approved yet: its next
// draft is the script alone, and approving that queues the video.
func (s *Story) AtScript(id uuid.UUID) bool {
	d, err := s.Deliverable(id)
	return err == nil && s.atScript(d)
}

func (s *Story) atScript(d *Deliverable) bool {
	// A short cut from a long video speaks the long video's approved
	// narration, so it has no script of its own to review.
	return d.IsVideo() && d.clip == nil && s.brief.Production.ReviewAt == pipelinedomain.ReviewScript && d.scriptApprovedAt.IsZero()
}

// StartDrafting hands a queued deliverable to the agent and returns the
// feedback to address (nil for a first draft). A deliverable already
// drafting starts over: its draft was interrupted (the worker stopped)
// and the job runs again.
func (s *Story) StartDrafting(id uuid.UUID, now time.Time) (*Feedback, error) {
	d, err := s.find(id, StatusQueued, StatusDrafting)
	if err != nil {
		return nil, err
	}
	d.set(StatusDrafting, now)
	s.touch(now)
	return d.pending, nil
}

// Drafted records the agent's output as the next revision, measured
// as m (its words and narrator; the length comes from the render).
// Videos go on to the preview render; articles, and a video's script
// awaiting approval, straight to review.
func (s *Story) Drafted(id uuid.UUID, artifacts map[string]string, session, notes string, m Measures, now time.Time) (Revision, error) {
	d, err := s.find(id, StatusDrafting)
	if err != nil {
		return Revision{}, err
	}
	d.revision++
	d.measures = Measures{Words: max(m.Words, 0), Narrator: m.Narrator}
	rev := Revision{
		DeliverableID: d.id, Number: d.revision, Feedback: d.pending,
		Artifacts: maps.Clone(artifacts), Notes: notes, Measures: d.measures, CreatedAt: timeutil.Normalize(now),
	}
	script := s.atScript(d)
	if script {
		rev.Stage = StageScript
	}
	d.pending, d.failure, d.failedStep = nil, "", ""
	if session != "" {
		d.session = session
	}
	if d.IsVideo() && !script {
		d.set(StatusRendering, now)
	} else {
		d.set(StatusReview, now)
	}
	s.touch(now)
	return rev, nil
}

// Edited records a reviewer's in-place edit, now of words words, as the
// next revision. With retime set (the narration changed), the
// deliverable goes back to the agent to re-narrate and re-time its
// visuals to the edited text, so its length is unknown until the next
// render; otherwise it stays in review with the video it had.
func (s *Story) Edited(id uuid.UUID, artifacts map[string]string, by uuid.UUID, retime *Feedback, words int, now time.Time) (Revision, error) {
	d, err := s.find(id, StatusReview)
	if err != nil {
		return Revision{}, err
	}
	if retime != nil && !d.IsVideo() {
		return Revision{}, apperror.New(apperror.ValidationFailed, "only a video is re-timed after an edit")
	}
	script := s.atScript(d)
	if retime != nil && script {
		return Revision{}, apperror.New(apperror.ValidationFailed, "a script in review is not narrated yet; there is nothing to re-time")
	}
	d.revision++
	d.measures.Words = max(words, 0)
	if retime != nil {
		d.measures.Duration = 0
	}
	rev := Revision{
		DeliverableID: d.id, Number: d.revision, Artifacts: maps.Clone(artifacts), EditedBy: by,
		Notes: "Edited in place.", Measures: d.measures, CreatedAt: timeutil.Normalize(now),
	}
	if script {
		rev.Stage = StageScript
	}
	if retime != nil {
		rev.Notes = "Narration edited in place; the agent re-times the video to it."
		d.pending = retime
		d.set(StatusQueued, now)
	} else {
		d.updatedAt = timeutil.Normalize(now)
	}
	s.touch(now)
	return rev, nil
}

// Rendered records a finished render of the given length (0 when it
// could not be measured): a preview puts the video in review, a final
// render makes it ready.
func (s *Story) Rendered(id uuid.UUID, length time.Duration, now time.Time) error {
	d, err := s.find(id, StatusRendering, StatusFinalizing)
	if err != nil {
		return err
	}
	if length > 0 {
		d.measures.Duration = length
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
// is live and they can link to it. Approving a video's script (AtScript)
// approves only the script: the video is queued to be built from it and
// comes back for review.
func (s *Story) Approve(id, by uuid.UUID, newID func() uuid.UUID, now time.Time) ([]*Deliverable, error) {
	d, err := s.find(id, StatusReview)
	if err != nil {
		return nil, err
	}
	if s.atScript(d) {
		d.scriptApprovedAt = timeutil.Normalize(now)
		d.set(StatusQueued, now)
		s.touch(now)
		return nil, nil
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
	d.fail(reason, now)
	s.touch(now)
	return nil
}

func (d *Deliverable) fail(reason string, now time.Time) {
	if len(reason) > 4000 {
		reason = reason[:4000]
	}
	d.failedStep, d.failure = d.status, reason
	d.set(StatusFailed, now)
}

// Step is the job that moves a deliverable forward.
type Step string

const (
	StepDraft   Step = "draft"
	StepPreview Step = "preview" // the render for review
	StepFinal   Step = "final"   // the render after approval
	StepPublish Step = "publish"
)

// Abandon fails deliverable id when job, the job running step for it,
// ended without finishing it (it expired, was cancelled or failed
// outright), so the deliverable does not wait for a job that is gone and
// a person can retry it. A deliverable that has moved on is left alone;
// Abandon reports whether it failed it.
func (s *Story) Abandon(id uuid.UUID, step Step, job uuid.UUID, reason string, now time.Time) (bool, error) {
	d, err := s.Deliverable(id)
	if err != nil {
		return false, err
	}
	var waiting bool
	switch step {
	case StepDraft:
		waiting = d.status == StatusQueued || d.status == StatusDrafting
	case StepPreview:
		waiting = d.status == StatusRendering
	case StepFinal:
		waiting = d.status == StatusFinalizing
	case StepPublish:
		waiting = d.status == StatusPublishing && d.publishJob == job
	default:
		return false, apperror.New(apperror.ValidationFailed, "unknown step "+string(step))
	}
	if !waiting {
		return false, nil
	}
	d.fail(reason, now)
	s.touch(now)
	return true, nil
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

// StartPublishing marks a ready deliverable as going out on behalf of
// job. The same job may start again on a deliverable it left publishing
// (its attempt was cut short); any other job is refused, so two jobs
// never publish one deliverable at once.
func (s *Story) StartPublishing(id, job uuid.UUID, now time.Time) error {
	if job == uuid.Nil {
		return apperror.New(apperror.ValidationFailed, "a publishing job is required")
	}
	d, err := s.find(id, StatusReady, StatusPublishing)
	if err != nil {
		return err
	}
	if d.status == StatusPublishing && d.publishJob != job {
		return apperror.New(apperror.InvalidTransition, "the "+string(d.kind)+" is being published by another job")
	}
	d.publishJob = job
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
