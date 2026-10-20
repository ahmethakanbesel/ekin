// Package pipeline defines the Pipeline aggregate: a standing order to
// produce videos and/or articles from a source, on a cron schedule or on
// demand.
package pipeline

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	voicedomain "github.com/ahmethakanbesel/ekin/internal/domain/voice/v1"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// MaxNameLength bounds a pipeline name, in runes.
const MaxNameLength = 120

// Status is whether a pipeline's schedule fires.
type Status string

const (
	StatusActive Status = "active"
	StatusPaused Status = "paused"
)

// Definition is the user-editable part of a pipeline, already parsed into
// valid value types. Schedule is nil for a manual-only pipeline.
type Definition struct {
	Name     string
	Language Language
	Outputs  Outputs
	Source   Source
	Schedule *Schedule
	// TemplateID is the house style its stories use; Nil takes the
	// workspace's default template unless NoTemplate is set.
	TemplateID uuid.UUID
	// NoTemplate makes its stories use no template, whatever the
	// workspace's default; TemplateID must then be Nil.
	NoTemplate bool
	// Voice overrides the workspace's narration voice; nil for none.
	Voice *voicedomain.Override
	// Production overrides how the stories are made (self-review rounds
	// among them); zero for the workspace defaults.
	Production Production
}

// Pipeline is the aggregate root.
type Pipeline struct {
	id          uuid.UUID
	workspaceID uuid.UUID
	def         Definition
	status      Status
	nextRunAt   time.Time // zero when paused or unscheduled
	lastRunAt   time.Time
	createdAt   time.Time
	updatedAt   time.Time
	version     int64 // 1 when new; the repository bumps it on every update
}

// New returns an active pipeline. A scheduled pipeline's first run is
// the schedule's first firing after now.
func New(id, workspaceID uuid.UUID, def Definition, now time.Time) (*Pipeline, error) {
	if id == uuid.Nil || workspaceID == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "pipeline and workspace ids are required")
	}
	def, err := checkDefinition(def)
	if err != nil {
		return nil, err
	}
	now = timeutil.Normalize(now)
	p := &Pipeline{
		id:          id,
		workspaceID: workspaceID,
		def:         def,
		status:      StatusActive,
		createdAt:   now,
		updatedAt:   now,
		version:     1,
	}
	p.planNextRun(now)
	return p, nil
}

// Redefine replaces the definition. A changed schedule restarts from now;
// an unchanged one keeps its pending slot.
func (p *Pipeline) Redefine(def Definition, now time.Time) error {
	def, err := checkDefinition(def)
	if err != nil {
		return err
	}
	scheduleChanged := !sameSchedule(p.def.Schedule, def.Schedule)
	p.def = def
	now = timeutil.Normalize(now)
	if scheduleChanged {
		p.planNextRun(now)
	}
	p.updatedAt = now
	return nil
}

// Pause stops scheduled runs. Manual runs stay possible.
func (p *Pipeline) Pause(now time.Time) error {
	if p.status == StatusPaused {
		return apperror.New(apperror.InvalidTransition, "pipeline is already paused")
	}
	p.status = StatusPaused
	p.nextRunAt = time.Time{}
	p.updatedAt = timeutil.Normalize(now)
	return nil
}

// Resume restarts scheduled runs from the next firing after now. Slots
// missed while paused are not caught up.
func (p *Pipeline) Resume(now time.Time) error {
	if p.status == StatusActive {
		return apperror.New(apperror.InvalidTransition, "pipeline is already active")
	}
	now = timeutil.Normalize(now)
	p.status = StatusActive
	p.planNextRun(now)
	p.updatedAt = now
	return nil
}

// Due reports whether a scheduled run is owed at now.
func (p *Pipeline) Due(now time.Time) bool {
	return p.status == StatusActive && !p.nextRunAt.IsZero() && !p.nextRunAt.After(now)
}

// Trigger consumes the due scheduled slot and plans the next one. Slots
// missed while the scheduler was down collapse into this one run rather
// than replaying a backlog: a box that was off for three nights produces
// one batch, not three.
func (p *Pipeline) Trigger(now time.Time) (Run, error) {
	if !p.Due(now) {
		return Run{}, apperror.New(apperror.InvalidTransition, "pipeline is not due")
	}
	now = timeutil.Normalize(now)
	run := Run{
		PipelineID:  p.id,
		WorkspaceID: p.workspaceID,
		Trigger:     TriggerSchedule,
		Slot:        p.nextRunAt,
		Count:       p.def.Schedule.Batch(),
	}
	p.lastRunAt = now
	p.planNextRun(now)
	p.updatedAt = now
	return run, nil
}

// RunNow starts an unscheduled run of count items, whatever the status.
// It does not move the schedule.
func (p *Pipeline) RunNow(count int, now time.Time) (Run, error) {
	if count < 1 || count > MaxBatch {
		return Run{}, apperror.New(apperror.ValidationFailed, "items per run must be between 1 and 20")
	}
	now = timeutil.Normalize(now)
	p.lastRunAt = now
	p.updatedAt = now
	return Run{
		PipelineID:  p.id,
		WorkspaceID: p.workspaceID,
		Trigger:     TriggerManual,
		Slot:        now,
		Count:       count,
	}, nil
}

// Upcoming lists the scheduled runs before until, starting with the
// planned next run (which may already be due). A paused or unscheduled
// pipeline has none.
func (p *Pipeline) Upcoming(until time.Time) []time.Time {
	if p.status != StatusActive || p.def.Schedule == nil {
		return nil
	}
	var out []time.Time
	for at := p.nextRunAt; !at.IsZero() && at.Before(until); at = p.def.Schedule.Next(at) {
		out = append(out, at)
	}
	return out
}

func (p *Pipeline) planNextRun(now time.Time) {
	p.nextRunAt = time.Time{}
	if p.status == StatusActive && p.def.Schedule != nil {
		p.nextRunAt = timeutil.Normalize(p.def.Schedule.Next(now))
	}
}

func (p *Pipeline) ID() uuid.UUID          { return p.id }
func (p *Pipeline) WorkspaceID() uuid.UUID { return p.workspaceID }
func (p *Pipeline) Name() string           { return p.def.Name }
func (p *Pipeline) Language() Language     { return p.def.Language }
func (p *Pipeline) Outputs() Outputs       { return p.def.Outputs }
func (p *Pipeline) Source() Source         { return p.def.Source }
func (p *Pipeline) TemplateID() uuid.UUID  { return p.def.TemplateID }

// Template is the pipeline's template choice, to resolve over the
// workspace's default.
func (p *Pipeline) Template() TemplateChoice {
	return TemplateChoice{ID: p.def.TemplateID, None: p.def.NoTemplate}
}

func (p *Pipeline) Voice() *voicedomain.Override {
	if p.def.Voice == nil {
		return nil
	}
	v := *p.def.Voice
	return &v
}
func (p *Pipeline) Production() Production { return p.def.Production }
func (p *Pipeline) Status() Status         { return p.status }
func (p *Pipeline) NextRunAt() time.Time   { return p.nextRunAt }
func (p *Pipeline) LastRunAt() time.Time   { return p.lastRunAt }
func (p *Pipeline) CreatedAt() time.Time   { return p.createdAt }
func (p *Pipeline) UpdatedAt() time.Time   { return p.updatedAt }
func (p *Pipeline) Version() int64         { return p.version }

// Schedule returns the schedule, if the pipeline has one.
func (p *Pipeline) Schedule() (Schedule, bool) {
	if p.def.Schedule == nil {
		return Schedule{}, false
	}
	return *p.def.Schedule, true
}

func checkDefinition(def Definition) (Definition, error) {
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return Definition{}, apperror.New(apperror.ValidationFailed, "pipeline name is required")
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return Definition{}, apperror.New(apperror.ValidationFailed, "pipeline name is too long")
	}
	def.Name = name
	if _, err := ParseLanguage(string(def.Language)); err != nil {
		return Definition{}, err
	}
	if def.Outputs.isZero() {
		return Definition{}, apperror.New(apperror.ValidationFailed, "outputs are required")
	}
	if def.Source.isZero() {
		return Definition{}, apperror.New(apperror.ValidationFailed, "source is required")
	}
	if article, ok := def.Outputs.Article(); ok && article.TranslatesTo(def.Language) {
		return Definition{}, apperror.New(apperror.ValidationFailed, "an article cannot be translated into its own language")
	}
	if err := def.Production.CheckFor(def.Outputs); err != nil {
		return Definition{}, err
	}
	if _, err := NewTemplateChoice(def.TemplateID, def.NoTemplate); err != nil {
		return Definition{}, err
	}
	return def, nil
}

func sameSchedule(a, b *Schedule) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// TriggerKind is why a run started.
type TriggerKind string

const (
	TriggerSchedule TriggerKind = "schedule"
	TriggerManual   TriggerKind = "manual"
)

// Run is one started pipeline run: Count items to produce.
type Run struct {
	PipelineID  uuid.UUID
	WorkspaceID uuid.UUID
	Trigger     TriggerKind
	Slot        time.Time
	Count       int
}

// DedupKey identifies a scheduled slot so it is enqueued at most once,
// even when two scheduler ticks race. Manual runs are never deduplicated.
func (r Run) DedupKey() string {
	if r.Trigger != TriggerSchedule {
		return ""
	}
	return "pipeline-run:" + r.PipelineID.String() + ":" + r.Slot.UTC().Format(time.RFC3339)
}
