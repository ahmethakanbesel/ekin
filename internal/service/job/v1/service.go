// Package job runs the job use cases: enqueue, claim, heartbeat,
// complete, cancel. Every transition goes through the aggregate inside a
// transaction, so two workers never both start one job.
package job

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/db"
	jobdomain "kurgu/internal/domain/job/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	"kurgu/pkg/id"
)

// abandonedSweepLimit bounds one sweep; the next sweep takes the rest.
const abandonedSweepLimit = 100

// Service runs job use cases.
type Service struct {
	repo  jobrepo.Repository
	tx    db.TxManager
	now   func() time.Time
	ended map[string]Ended
}

// Ended is told about a job that ended without succeeding: it failed
// for good, expired or was cancelled. It runs inside the transaction
// that ends the job, so whatever the job left waiting is settled with
// it or not at all.
type Ended func(ctx context.Context, j *jobdomain.Job) error

// NewService returns a Service.
func NewService(repo jobrepo.Repository, tx db.TxManager) *Service {
	return &Service{repo: repo, tx: tx, now: time.Now, ended: map[string]Ended{}}
}

// OnEnded calls fn for every job of kind that ends without succeeding.
// Register before the service is used.
func (s *Service) OnEnded(kind string, fn Ended) *Service {
	s.ended[kind] = fn
	return s
}

// settle tells the kind's Ended about j when j ended without succeeding.
func (s *Service) settle(ctx context.Context, j *jobdomain.Job) error {
	if st := j.Status(); st != jobdomain.StatusFailed && st != jobdomain.StatusCancelled {
		return nil
	}
	if fn, ok := s.ended[j.Kind()]; ok {
		return fn(ctx, j)
	}
	return nil
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Enqueue adds a job, reporting false when spec's unique key was taken.
// Unless spec names a trace, the job joins the one ctx carries.
func (s *Service) Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error) {
	if spec.TraceID == uuid.Nil {
		spec.TraceID = jobdomain.TraceFrom(ctx)
	}
	j, err := jobdomain.New(id.New(), spec, s.now())
	if err != nil {
		return nil, false, err
	}
	inserted, err := s.repo.Enqueue(ctx, j)
	if err != nil {
		return nil, false, err
	}
	return j, inserted, nil
}

// Claim starts the next claimable job for owner, or returns nil.
func (s *Service) Claim(ctx context.Context, queues []jobdomain.Queue, kinds []string, owner string, lease time.Duration) (*jobdomain.Job, error) {
	var claimed *jobdomain.Job
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		now := s.now()
		j, err := s.repo.NextClaimable(ctx, queues, kinds, now)
		if err != nil || j == nil {
			return err
		}
		if err := j.Start(owner, lease, now); err != nil {
			return err
		}
		if err := s.repo.Save(ctx, j); err != nil {
			return err
		}
		claimed = j
		return nil
	})
	return claimed, err
}

// Heartbeat extends owner's lease on job id. It fails with
// InvalidTransition once the job was cancelled or taken over.
func (s *Service) Heartbeat(ctx context.Context, jobID uuid.UUID, owner string, lease time.Duration) error {
	return s.mutate(ctx, jobID, func(j *jobdomain.Job, now time.Time) error {
		return j.Heartbeat(owner, lease, now)
	})
}

// Complete records the outcome of owner's attempt: success for a nil
// cause, otherwise a failure that may be retried.
func (s *Service) Complete(ctx context.Context, jobID uuid.UUID, owner string, cause error) error {
	return s.mutate(ctx, jobID, func(j *jobdomain.Job, now time.Time) error {
		if cause == nil {
			return j.Succeed(owner, now)
		}
		return j.Fail(owner, cause, now)
	})
}

// Release returns owner's job to the queue without spending the attempt.
func (s *Service) Release(ctx context.Context, jobID uuid.UUID, owner string) error {
	return s.mutate(ctx, jobID, func(j *jobdomain.Job, now time.Time) error {
		return j.Release(owner, now)
	})
}

// Cancel stops a workspace's job.
func (s *Service) Cancel(ctx context.Context, workspaceID, jobID uuid.UUID) (*jobdomain.Job, error) {
	var out *jobdomain.Job
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		j, err := s.repo.Get(ctx, workspaceID, jobID)
		if err != nil {
			return err
		}
		if err := j.Cancel(s.now()); err != nil {
			return err
		}
		out = j
		if err := s.repo.Save(ctx, j); err != nil {
			return err
		}
		return s.settle(ctx, j)
	})
	return out, err
}

// ExpireAbandoned fails jobs whose worker vanished on their last attempt.
// Each job ends in a transaction of its own, so one that cannot be
// settled does not hold up the rest; it is tried again next sweep.
func (s *Service) ExpireAbandoned(ctx context.Context) (int, error) {
	jobs, err := s.repo.ListAbandoned(ctx, s.now(), abandonedSweepLimit)
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, candidate := range jobs {
		expired := false
		err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
			j, err := s.repo.GetByID(ctx, candidate.ID())
			if err != nil {
				return err
			}
			now := s.now()
			if !j.Abandoned(now) {
				return nil // its worker came back meanwhile
			}
			if err := j.Expire(now); err != nil {
				return err
			}
			if err := s.repo.Save(ctx, j); err != nil {
				return err
			}
			expired = true
			return s.settle(ctx, j)
		})
		switch {
		case err != nil:
			errs = append(errs, err)
		case expired:
			n++
		}
	}
	return n, errors.Join(errs...)
}

// Get returns one of a workspace's jobs.
func (s *Service) Get(ctx context.Context, workspaceID, jobID uuid.UUID) (*jobdomain.Job, error) {
	return s.repo.Get(ctx, workspaceID, jobID)
}

// List returns every job of a workspace's latest requests, newest first.
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]*jobdomain.Job, error) {
	return s.repo.List(ctx, workspaceID, f)
}

func (s *Service) mutate(ctx context.Context, jobID uuid.UUID, fn func(j *jobdomain.Job, now time.Time) error) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		j, err := s.repo.GetByID(ctx, jobID)
		if err != nil {
			return err
		}
		if err := fn(j, s.now()); err != nil {
			return err
		}
		if err := s.repo.Save(ctx, j); err != nil {
			return err
		}
		return s.settle(ctx, j)
	})
}

// Filter narrows List.
type Filter = jobrepo.Filter

// ByID returns a job by id, whatever its workspace (worker API).
func (s *Service) ByID(ctx context.Context, jobID uuid.UUID) (*jobdomain.Job, error) {
	return s.repo.GetByID(ctx, jobID)
}
