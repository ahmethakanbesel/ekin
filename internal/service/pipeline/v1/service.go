// Package pipeline runs pipeline use cases and turns due schedules into
// pipeline.run jobs.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	voicedomain "kurgu/internal/domain/voice/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
)

// KindRun is the job kind of a pipeline run.
const KindRun = "pipeline.run"

// dueBatch bounds how many pipelines one scheduler tick triggers.
const dueBatch = 50

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// RunPayload is the pipeline.run job payload.
type RunPayload struct {
	PipelineID  uuid.UUID `json:"pipelineId"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
	Trigger     string    `json:"trigger"`
	Slot        time.Time `json:"slot"`
	Count       int       `json:"count"`
}

// DocumentChecker verifies knowledge references (the knowledge service).
type DocumentChecker interface {
	CheckRange(ctx context.Context, workspaceID, documentID uuid.UUID, from, to int) error
}

// TemplateChecker verifies a pipeline's template (the template service).
type TemplateChecker interface {
	Check(ctx context.Context, workspaceID, templateID uuid.UUID, kinds []pipelinedomain.Kind) error
}

// VoiceChecker verifies a pipeline's voice override (the voice service).
type VoiceChecker interface {
	CheckOverride(ctx context.Context, language string, o voicedomain.Override) error
}

// Service runs pipeline use cases.
type Service struct {
	voices     VoiceChecker
	templates  TemplateChecker
	repo       pipelinerepo.Repository
	workspaces workspacerepo.Repository
	jobs       Enqueuer
	documents  DocumentChecker
	tx         db.TxManager
	now        func() time.Time
}

// NewService returns a Service.
func NewService(repo pipelinerepo.Repository, workspaces workspacerepo.Repository, jobs Enqueuer, tx db.TxManager) *Service {
	return &Service{repo: repo, workspaces: workspaces, jobs: jobs, tx: tx, now: time.Now}
}

// WithVoices makes Create and Redefine check voice overrides.
func (s *Service) WithVoices(v VoiceChecker) *Service {
	s.voices = v
	return s
}

// WithTemplates makes Create and Redefine check the chosen template.
func (s *Service) WithTemplates(t TemplateChecker) *Service {
	s.templates = t
	return s
}

// WithDocuments makes Create and Redefine check knowledge references
// against the knowledge base.
func (s *Service) WithDocuments(d DocumentChecker) *Service {
	s.documents = d
	return s
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Create creates a pipeline in workspaceID.
func (s *Service) Create(ctx context.Context, workspaceID uuid.UUID, def pipelinedomain.Definition) (*pipelinedomain.Pipeline, error) {
	if _, err := s.workspaces.Get(ctx, workspaceID); err != nil {
		return nil, err
	}
	if err := s.checkDefinition(ctx, workspaceID, def); err != nil {
		return nil, err
	}
	p, err := pipelinedomain.New(id.New(), workspaceID, def, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Get returns one pipeline.
func (s *Service) Get(ctx context.Context, workspaceID, pipelineID uuid.UUID) (*pipelinedomain.Pipeline, error) {
	return s.repo.Get(ctx, workspaceID, pipelineID)
}

// List returns a workspace's pipelines.
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID) ([]*pipelinedomain.Pipeline, error) {
	return s.repo.List(ctx, workspaceID)
}

// UpcomingRun is one scheduled run ahead.
type UpcomingRun struct {
	PipelineID uuid.UUID
	At         time.Time
	Count      int
}

// MaxUpcomingDays bounds how far ahead Upcoming looks.
const MaxUpcomingDays = 14

// Upcoming returns the workspace's scheduled runs in the next days days,
// soonest first, and the end of that window.
func (s *Service) Upcoming(ctx context.Context, workspaceID uuid.UUID, days int) ([]UpcomingRun, time.Time, error) {
	if days < 1 || days > MaxUpcomingDays {
		return nil, time.Time{}, apperror.New(apperror.ValidationFailed, fmt.Sprintf("days must be between 1 and %d", MaxUpcomingDays))
	}
	until := s.now().UTC().Add(time.Duration(days) * 24 * time.Hour)
	ps, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return nil, time.Time{}, err
	}
	var out []UpcomingRun
	for _, p := range ps {
		sch, _ := p.Schedule()
		for _, at := range p.Upcoming(until) {
			out = append(out, UpcomingRun{PipelineID: p.ID(), At: at, Count: sch.Batch()})
		}
	}
	slices.SortStableFunc(out, func(a, b UpcomingRun) int { return a.At.Compare(b.At) })
	return out, until, nil
}

// Redefine replaces a pipeline's definition. version is the version the
// caller read; a newer stored version fails with StaleVersion instead of
// overwriting someone else's edit.
func (s *Service) Redefine(ctx context.Context, workspaceID, pipelineID uuid.UUID, version int64, def pipelinedomain.Definition) (*pipelinedomain.Pipeline, error) {
	if err := s.checkDefinition(ctx, workspaceID, def); err != nil {
		return nil, err
	}
	return s.change(ctx, workspaceID, pipelineID, version, func(_ context.Context, p *pipelinedomain.Pipeline, now time.Time) error {
		return p.Redefine(def, now)
	})
}

// Pause stops a pipeline's schedule.
func (s *Service) Pause(ctx context.Context, workspaceID, pipelineID uuid.UUID) (*pipelinedomain.Pipeline, error) {
	return s.change(ctx, workspaceID, pipelineID, 0, func(_ context.Context, p *pipelinedomain.Pipeline, now time.Time) error {
		return p.Pause(now)
	})
}

// Resume restarts a pipeline's schedule.
func (s *Service) Resume(ctx context.Context, workspaceID, pipelineID uuid.UUID) (*pipelinedomain.Pipeline, error) {
	return s.change(ctx, workspaceID, pipelineID, 0, func(_ context.Context, p *pipelinedomain.Pipeline, now time.Time) error {
		return p.Resume(now)
	})
}

func (s *Service) checkDefinition(ctx context.Context, workspaceID uuid.UUID, def pipelinedomain.Definition) error {
	if def.Voice != nil && s.voices != nil {
		if err := s.voices.CheckOverride(ctx, string(def.Language), *def.Voice); err != nil {
			return err
		}
	}
	if def.TemplateID != uuid.Nil && s.templates != nil {
		var kinds []pipelinedomain.Kind
		for _, k := range []pipelinedomain.Kind{pipelinedomain.KindVideo, pipelinedomain.KindArticle} {
			if def.Outputs.Produces(k) {
				kinds = append(kinds, k)
			}
		}
		if err := s.templates.Check(ctx, workspaceID, def.TemplateID, kinds); err != nil {
			return err
		}
	}
	return s.checkRefs(ctx, workspaceID, def.Source)
}

func (s *Service) checkRefs(ctx context.Context, workspaceID uuid.UUID, src pipelinedomain.Source) error {
	if s.documents == nil {
		return nil
	}
	for _, ref := range src.Refs() {
		if err := s.documents.CheckRange(ctx, workspaceID, ref.DocumentID(), ref.Pages().From(), ref.Pages().To()); err != nil {
			return err
		}
	}
	return nil
}

// PipelinesUsing names the workspace's pipelines that reference a
// document.
func (s *Service) PipelinesUsing(ctx context.Context, workspaceID, documentID uuid.UUID) ([]string, error) {
	all, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range all {
		for _, ref := range p.Source().Refs() {
			if ref.DocumentID() == documentID {
				names = append(names, p.Name())
				break
			}
		}
	}
	return names, nil
}

// Delete removes a pipeline.
func (s *Service) Delete(ctx context.Context, workspaceID, pipelineID uuid.UUID) error {
	return s.repo.Delete(ctx, workspaceID, pipelineID)
}

// RunNow starts a manual run of count items and returns its job.
func (s *Service) RunNow(ctx context.Context, workspaceID, pipelineID uuid.UUID, count int) (*jobdomain.Job, error) {
	var out *jobdomain.Job
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.repo.Get(ctx, workspaceID, pipelineID)
		if err != nil {
			return err
		}
		run, err := p.RunNow(count, s.now())
		if err != nil {
			return err
		}
		if _, err := s.repo.Update(ctx, p); err != nil {
			return err
		}
		out, _, err = s.enqueue(ctx, run)
		return err
	})
	return out, err
}

// TriggerDue enqueues one pipeline.run job for every pipeline whose
// scheduled slot has come, and reports how many it triggered. Each
// pipeline is triggered in its own transaction: the slot is consumed and
// the job enqueued together or not at all, and the job's unique key stops
// a racing scheduler from enqueueing the same slot twice.
func (s *Service) TriggerDue(ctx context.Context) (int, error) {
	due, err := s.repo.ListDue(ctx, s.now(), dueBatch)
	if err != nil {
		return 0, err
	}
	triggered := 0
	for _, candidate := range due {
		err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
			p, err := s.repo.Get(ctx, candidate.WorkspaceID(), candidate.ID())
			if err != nil {
				return err
			}
			now := s.now()
			if !p.Due(now) {
				return nil
			}
			run, err := p.Trigger(now)
			if err != nil {
				return err
			}
			if _, err := s.repo.Update(ctx, p); err != nil {
				return err
			}
			if _, inserted, err := s.enqueue(ctx, run); err != nil || !inserted {
				return err
			}
			triggered++
			return nil
		})
		switch {
		case err == nil:
		case apperror.Is(err, apperror.StaleVersion), apperror.Is(err, apperror.PipelineNotFound):
			// Edited or deleted since ListDue; the next tick sees the new state.
		default:
			return triggered, fmt.Errorf("trigger pipeline %s: %w", candidate.ID(), err)
		}
	}
	return triggered, nil
}

func (s *Service) enqueue(ctx context.Context, run pipelinedomain.Run) (*jobdomain.Job, bool, error) {
	payload, err := json.Marshal(RunPayload{
		PipelineID:  run.PipelineID,
		WorkspaceID: run.WorkspaceID,
		Trigger:     string(run.Trigger),
		Slot:        run.Slot,
		Count:       run.Count,
	})
	if err != nil {
		return nil, false, fmt.Errorf("encode run payload: %w", err)
	}
	return s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: run.WorkspaceID,
		Queue:       jobdomain.QueueControl,
		Kind:        KindRun,
		Payload:     payload,
		UniqueKey:   run.DedupKey(),
	})
}

// change loads, mutates and saves a pipeline in one transaction, so a
// change made meanwhile (the scheduler consuming a slot) is applied on,
// not refused as stale. A non-zero version is the one the caller read
// and must still be the stored one.
func (s *Service) change(ctx context.Context, workspaceID, pipelineID uuid.UUID, version int64, fn func(ctx context.Context, p *pipelinedomain.Pipeline, now time.Time) error) (*pipelinedomain.Pipeline, error) {
	var out *pipelinedomain.Pipeline
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.repo.Get(ctx, workspaceID, pipelineID)
		if err != nil {
			return err
		}
		if version != 0 && p.Version() != version {
			return apperror.New(apperror.StaleVersion, "pipeline was changed by someone else; reload and retry")
		}
		if err := fn(ctx, p, s.now()); err != nil {
			return err
		}
		out, err = s.repo.Update(ctx, p)
		return err
	})
	return out, err
}
