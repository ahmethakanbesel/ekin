// Package pipeline runs pipeline use cases and turns due schedules into
// pipeline.run jobs.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
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

// Service runs pipeline use cases.
type Service struct {
	repo       pipelinerepo.Repository
	workspaces workspacerepo.Repository
	jobs       Enqueuer
	tx         db.TxManager
	now        func() time.Time
}

// NewService returns a Service.
func NewService(repo pipelinerepo.Repository, workspaces workspacerepo.Repository, jobs Enqueuer, tx db.TxManager) *Service {
	return &Service{repo: repo, workspaces: workspaces, jobs: jobs, tx: tx, now: time.Now}
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

// Redefine replaces a pipeline's definition. version is the version the
// caller read; a newer stored version fails with StaleVersion instead of
// overwriting someone else's edit.
func (s *Service) Redefine(ctx context.Context, workspaceID, pipelineID uuid.UUID, version int64, def pipelinedomain.Definition) (*pipelinedomain.Pipeline, error) {
	return s.change(ctx, workspaceID, pipelineID, version, func(p *pipelinedomain.Pipeline, now time.Time) error {
		return p.Redefine(def, now)
	})
}

// Pause stops a pipeline's schedule.
func (s *Service) Pause(ctx context.Context, workspaceID, pipelineID uuid.UUID) (*pipelinedomain.Pipeline, error) {
	return s.change(ctx, workspaceID, pipelineID, 0, func(p *pipelinedomain.Pipeline, now time.Time) error {
		return p.Pause(now)
	})
}

// Resume restarts a pipeline's schedule.
func (s *Service) Resume(ctx context.Context, workspaceID, pipelineID uuid.UUID) (*pipelinedomain.Pipeline, error) {
	return s.change(ctx, workspaceID, pipelineID, 0, func(p *pipelinedomain.Pipeline, now time.Time) error {
		return p.Resume(now)
	})
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

// change loads, mutates and saves a pipeline. A non-zero version must
// match the stored one.
func (s *Service) change(ctx context.Context, workspaceID, pipelineID uuid.UUID, version int64, fn func(*pipelinedomain.Pipeline, time.Time) error) (*pipelinedomain.Pipeline, error) {
	p, err := s.repo.Get(ctx, workspaceID, pipelineID)
	if err != nil {
		return nil, err
	}
	if version != 0 && p.Version() != version {
		return nil, apperror.New(apperror.StaleVersion, "pipeline was changed by someone else; reload and retry")
	}
	if err := fn(p, s.now()); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, p)
}
