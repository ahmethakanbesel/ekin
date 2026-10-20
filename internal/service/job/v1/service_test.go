package job_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
)

// OnEnded hears about every job that ends without succeeding, inside the
// transaction that ends it: when it fails, the job does not end either.
func TestOnEndedSettlesJobsThatEndWithoutSucceeding(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	errBoom := errors.New("boom")
	tests := []struct {
		name      string
		end       func(f fixture, j *jobdomain.Job) error
		settleErr error
		want      jobdomain.Status
		settled   bool
	}{
		{name: "succeeded", end: complete(nil), want: jobdomain.StatusSucceeded},
		{name: "failed, retry left", end: complete(errors.New("transient")), want: jobdomain.StatusQueued},
		{name: "failed for good", end: complete(jobdomain.Permanent(errors.New("bad input"))), want: jobdomain.StatusFailed, settled: true},
		{name: "cancelled", end: cancel, want: jobdomain.StatusCancelled, settled: true},
		{name: "expired", end: expire(t0), want: jobdomain.StatusFailed, settled: true},
		{name: "settling fails: the cancel is undone", end: cancel, settleErr: errBoom, want: jobdomain.StatusRunning, settled: true},
		{name: "settling fails: the job stays abandoned", end: expire(t0), settleErr: errBoom, want: jobdomain.StatusRunning, settled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.svc.WithClock(func() time.Time { return t0 })
			var settled []jobdomain.Status
			f.svc.OnEnded("step", func(_ context.Context, j *jobdomain.Job) error {
				settled = append(settled, j.Status())
				return tt.settleErr
			})
			j := f.enqueue(t, jobdomain.QueueIO, "step")
			if _, err := f.svc.Claim(ctx, []jobdomain.Queue{jobdomain.QueueIO}, []string{"step"}, "w", time.Minute); err != nil {
				t.Fatal(err)
			}
			err := tt.end(f, j)
			if tt.settleErr != nil && !errors.Is(err, tt.settleErr) {
				t.Fatalf("end = %v, want the settling error", err)
			}
			if tt.settleErr == nil && err != nil {
				t.Fatal(err)
			}
			if (len(settled) == 1) != tt.settled {
				t.Fatalf("settled %v, want settled = %v", settled, tt.settled)
			}
			got, _ := f.svc.Get(ctx, f.ws, j.ID())
			if got.Status() != tt.want {
				t.Fatalf("job is %s, want %s", got.Status(), tt.want)
			}
		})
	}
}

func complete(cause error) func(fixture, *jobdomain.Job) error {
	return func(f fixture, j *jobdomain.Job) error {
		// The first of the fixture's two attempts.
		return f.svc.Complete(context.Background(), j.ID(), "w", cause)
	}
}

func cancel(f fixture, j *jobdomain.Job) error {
	_, err := f.svc.Cancel(context.Background(), f.ws, j.ID())
	return err
}

// expire lets the lease run out on the last attempt and sweeps.
func expire(t0 time.Time) func(fixture, *jobdomain.Job) error {
	return func(f fixture, _ *jobdomain.Job) error {
		ctx := context.Background()
		later := t0.Add(2 * time.Minute)
		f.svc.WithClock(func() time.Time { return later })
		if _, err := f.svc.Claim(ctx, []jobdomain.Queue{jobdomain.QueueIO}, []string{"step"}, "w2", time.Minute); err != nil {
			return err // the second, last attempt
		}
		f.svc.WithClock(func() time.Time { return later.Add(2 * time.Minute) })
		n, err := f.svc.ExpireAbandoned(ctx)
		if err == nil && n != 1 {
			return apperror.New(apperror.InternalError, "nothing expired")
		}
		return err
	}
}
