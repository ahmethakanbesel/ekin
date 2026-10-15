package job_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	job "kurgu/internal/domain/job/v1"
)

var t0 = time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC)

const lease = 2 * time.Minute

func newJob(t *testing.T, maxAttempts int) *job.Job {
	t.Helper()
	j, err := job.New(uuid.New(), job.Spec{
		WorkspaceID: uuid.New(),
		Queue:       job.QueueRender,
		Kind:        "story.render",
		Payload:     json.RawMessage(`{"storyId":"x"}`),
		MaxAttempts: maxAttempts,
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestNewValidates(t *testing.T) {
	ws := uuid.New()
	tests := []struct {
		name    string
		spec    job.Spec
		wantErr bool
	}{
		{"ok", job.Spec{WorkspaceID: ws, Queue: job.QueueIO, Kind: "k"}, false},
		{"no workspace", job.Spec{Queue: job.QueueIO, Kind: "k"}, true},
		{"unknown queue", job.Spec{WorkspaceID: ws, Queue: "gpu", Kind: "k"}, true},
		{"blank kind", job.Spec{WorkspaceID: ws, Queue: job.QueueIO, Kind: " "}, true},
		{"bad json", job.Spec{WorkspaceID: ws, Queue: job.QueueIO, Kind: "k", Payload: json.RawMessage(`{`)}, true},
		{"too many attempts", job.Spec{WorkspaceID: ws, Queue: job.QueueIO, Kind: "k", MaxAttempts: 11}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := job.New(uuid.New(), tt.spec, t0)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 30 * time.Second},
		{1, 30 * time.Second},
		{2, time.Minute},
		{3, 2 * time.Minute},
		{7, 32 * time.Minute},
		{8, time.Hour}, // 64m, capped
		{50, time.Hour},
	}
	for _, tt := range tests {
		if got := job.Backoff(tt.attempt); got != tt.want {
			t.Errorf("Backoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestClaimable(t *testing.T) {
	delayed, _ := job.New(uuid.New(), job.Spec{WorkspaceID: uuid.New(), Queue: job.QueueIO, Kind: "k", RunAfter: t0.Add(time.Hour)}, t0)
	if delayed.Claimable(t0) {
		t.Fatal("job claimable before run_after")
	}
	if !delayed.Claimable(t0.Add(time.Hour)) {
		t.Fatal("job not claimable at run_after")
	}

	held := newJob(t, 2)
	if err := held.Start("a", lease, t0); err != nil {
		t.Fatal(err)
	}
	if held.Claimable(t0.Add(lease)) {
		t.Fatal("job claimable while its lease runs")
	}
	if !held.Claimable(t0.Add(lease + time.Millisecond)) {
		t.Fatal("expired lease with attempts left is not reclaimable")
	}
	if err := held.Start("b", lease, t0.Add(lease+time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if held.Attempt() != 2 {
		t.Fatalf("attempt = %d, want 2", held.Attempt())
	}
	late := t0.Add(3 * lease)
	if held.Claimable(late) || !held.Abandoned(late) {
		t.Fatal("last attempt's expired lease should be abandoned, not reclaimable")
	}
	if err := held.Expire(late); err != nil || held.Status() != job.StatusFailed {
		t.Fatalf("expire: err = %v, status = %s", err, held.Status())
	}
}

func TestFailRetriesThenGivesUp(t *testing.T) {
	j := newJob(t, 2)
	boom := errors.New("ffmpeg exited 1")

	_ = j.Start("w", lease, t0)
	if err := j.Fail("w", boom, t0); err != nil {
		t.Fatal(err)
	}
	if j.Status() != job.StatusQueued || !j.RunAfter().Equal(t0.Add(30*time.Second)) || j.LeaseOwner() != "" {
		t.Fatalf("after first failure: status=%s runAfter=%v owner=%q", j.Status(), j.RunAfter(), j.LeaseOwner())
	}

	at := j.RunAfter()
	_ = j.Start("w", lease, at)
	_ = j.Fail("w", boom, at)
	if j.Status() != job.StatusFailed || j.LastError() != boom.Error() || j.FinishedAt().IsZero() {
		t.Fatalf("after last attempt: status=%s err=%q", j.Status(), j.LastError())
	}
}

func TestPermanentFailureSkipsRetries(t *testing.T) {
	j := newJob(t, 5)
	_ = j.Start("w", lease, t0)
	if err := j.Fail("w", job.Permanent(errors.New("bad payload")), t0); err != nil {
		t.Fatal(err)
	}
	if j.Status() != job.StatusFailed {
		t.Fatalf("status = %s, want failed", j.Status())
	}
}

func TestOnlyTheHolderMayFinish(t *testing.T) {
	ops := map[string]func(j *job.Job) error{
		"succeed":   func(j *job.Job) error { return j.Succeed("intruder", t0) },
		"fail":      func(j *job.Job) error { return j.Fail("intruder", errors.New("x"), t0) },
		"heartbeat": func(j *job.Job) error { return j.Heartbeat("intruder", lease, t0) },
		"release":   func(j *job.Job) error { return j.Release("intruder", t0) },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			j := newJob(t, 3)
			_ = j.Start("owner", lease, t0)
			if err := op(j); !apperror.Is(err, apperror.InvalidTransition) {
				t.Fatalf("err = %v, want InvalidTransition", err)
			}
		})
	}
}

func TestReleaseRefundsTheAttempt(t *testing.T) {
	j := newJob(t, 1)
	_ = j.Start("w", lease, t0)
	if err := j.Release("w", t0); err != nil {
		t.Fatal(err)
	}
	if j.Status() != job.StatusQueued || j.Attempt() != 0 || !j.Claimable(t0) {
		t.Fatalf("status=%s attempt=%d", j.Status(), j.Attempt())
	}
}

func TestCancel(t *testing.T) {
	j := newJob(t, 3)
	_ = j.Start("w", lease, t0)
	if err := j.Cancel(t0); err != nil {
		t.Fatal(err)
	}
	if err := j.Heartbeat("w", lease, t0); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("heartbeat after cancel err = %v", err)
	}
	if err := j.Cancel(t0); !apperror.Is(err, apperror.InvalidTransition) {
		t.Fatalf("second cancel err = %v", err)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	j := newJob(t, 3)
	_ = j.Start("w", lease, t0)
	got, err := job.Rehydrate(j.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Snapshot(), j.Snapshot()) {
		t.Fatalf("got %+v\nwant %+v", got.Snapshot(), j.Snapshot())
	}
}

func TestTrace(t *testing.T) {
	j := newJob(t, 1)
	if j.TraceID() != j.ID() {
		t.Fatalf("a job without a trace starts its own: %s", j.TraceID())
	}
	parent := uuid.New()
	child, err := job.New(uuid.New(), job.Spec{WorkspaceID: uuid.New(), Queue: job.QueueIO, Kind: "k", TraceID: parent}, t0)
	if err != nil || child.TraceID() != parent {
		t.Fatalf("child trace = %s, %v", child.TraceID(), err)
	}
	if got := job.TraceFrom(job.WithTrace(context.Background(), parent)); got != parent {
		t.Fatalf("trace from ctx = %s", got)
	}
	if got := job.TraceFrom(job.WithTrace(context.Background(), uuid.Nil)); got != uuid.Nil {
		t.Fatalf("nil trace stored: %s", got)
	}
}

func TestStartRecordsTheAttemptStart(t *testing.T) {
	j := newJob(t, 2)
	if !j.StartedAt().IsZero() {
		t.Fatal("queued job has a start")
	}
	_ = j.Start("w", lease, t0)
	_ = j.Fail("w", errors.New("x"), t0.Add(time.Minute))
	retry := t0.Add(time.Hour)
	_ = j.Start("w", lease, retry)
	if !j.StartedAt().Equal(retry) {
		t.Fatalf("started at %v, want the latest attempt's %v", j.StartedAt(), retry)
	}
}
