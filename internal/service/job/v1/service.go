// Package job runs the job use cases: enqueue, claim, heartbeat,
// complete, cancel. Every transition goes through the aggregate inside a
// transaction, so two workers never both start one job.
package job

import (
	"context"
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
	repo jobrepo.Repository
	tx   db.TxManager
	now  func() time.Time
}

// NewService returns a Service.
func NewService(repo jobrepo.Repository, tx db.TxManager) *Service {
	return &Service{repo: repo, tx: tx, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Enqueue adds a job, reporting false when spec's unique key was taken.
func (s *Service) Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error) {
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
		return s.repo.Save(ctx, j)
	})
	return out, err
}

// ExpireAbandoned fails jobs whose worker vanished on their last attempt.
func (s *Service) ExpireAbandoned(ctx context.Context) (int, error) {
	n := 0
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		now := s.now()
		jobs, err := s.repo.ListAbandoned(ctx, now, abandonedSweepLimit)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if err := j.Expire(now); err != nil {
				return err
			}
			if err := s.repo.Save(ctx, j); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// Get returns one of a workspace's jobs.
func (s *Service) Get(ctx context.Context, workspaceID, jobID uuid.UUID) (*jobdomain.Job, error) {
	return s.repo.Get(ctx, workspaceID, jobID)
}

// List returns a workspace's jobs, newest first.
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID, f jobrepo.Filter) ([]*jobdomain.Job, error) {
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
		return s.repo.Save(ctx, j)
	})
}
