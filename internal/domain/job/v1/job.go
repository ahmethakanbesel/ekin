// Package job defines the unit of background work: a payload of a given
// kind, on a queue, with a lease-based claim and bounded retries.
package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/pkg/timeutil"
)

// Queue groups jobs by the resource they consume. Each host sets a
// concurrency per queue; zero means the host does not serve it.
type Queue string

const (
	// QueueControl runs light orchestration: picking ideas, fanning out.
	QueueControl Queue = "control"
	QueueAgent   Queue = "agent"
	QueueTTS     Queue = "tts"
	QueueRender  Queue = "render"
	QueueExtract Queue = "extract"
	QueueIO      Queue = "io"
)

// Queues lists every queue.
var Queues = []Queue{QueueControl, QueueAgent, QueueTTS, QueueRender, QueueExtract, QueueIO}

// ParseQueue parses a queue name.
func ParseQueue(s string) (Queue, error) {
	for _, q := range Queues {
		if string(q) == s {
			return q, nil
		}
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown queue")
}

// DefaultHeavy lists the queues whose jobs load the CPU enough that a
// host may run only one of them at a time, unless its profile says
// otherwise.
var DefaultHeavy = []Queue{QueueAgent, QueueTTS, QueueRender}

// Heavy reports whether q is heavy by default.
func (q Queue) Heavy() bool { return slices.Contains(DefaultHeavy, q) }

// Status is a job's lifecycle state.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// ParseStatus parses a status name.
func ParseStatus(s string) (Status, error) {
	switch st := Status(s); st {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled:
		return st, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown job status")
}

// Terminal reports whether no further transition is possible.
func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCancelled
}

// Limits.
const (
	DefaultMaxAttempts = 3
	MaxMaxAttempts     = 10
	MaxPayloadBytes    = 1 << 20
	MaxKindLength      = 100
	maxErrorLength     = 4000
	baseBackoff        = 30 * time.Second
	maxBackoff         = time.Hour
)

// Backoff is the delay before retry number attempt+1: 30s, 1m, 2m, …,
// capped at an hour.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := baseBackoff
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= maxBackoff {
			return maxBackoff
		}
	}
	return d
}

// ErrPermanent marks a handler error that retrying cannot fix.
var ErrPermanent = errors.New("permanent failure")

// Permanent wraps err so Fail skips the remaining attempts.
func Permanent(err error) error {
	return fmt.Errorf("%w: %w", ErrPermanent, err)
}

// Spec is what a caller asks to enqueue.
type Spec struct {
	WorkspaceID uuid.UUID
	Queue       Queue
	Kind        string
	Payload     json.RawMessage
	// UniqueKey, when set, makes the enqueue a no-op if a job with the
	// same key already exists in any state.
	UniqueKey string
	// ConcurrencyKey, when set, runs the job alone among the jobs sharing
	// it: it is not claimed while another of them holds a live lease
	// (e.g. one run of a pipeline at a time).
	ConcurrencyKey string
	MaxAttempts    int       // 0 means DefaultMaxAttempts
	RunAfter       time.Time // zero means now
	// TraceID joins the job to the request that caused it (the trace's
	// first job). Nil starts a new trace: the job's own id.
	TraceID uuid.UUID
}

type traceKey struct{}

// WithTrace returns ctx carrying trace, so jobs enqueued under it join
// that request: the runner sets it while a job runs, and a story's steps
// set their story's.
func WithTrace(ctx context.Context, trace uuid.UUID) context.Context {
	if trace == uuid.Nil {
		return ctx
	}
	return context.WithValue(ctx, traceKey{}, trace)
}

// TraceFrom returns the trace ctx carries, or Nil.
func TraceFrom(ctx context.Context) uuid.UUID {
	t, _ := ctx.Value(traceKey{}).(uuid.UUID)
	return t
}

// Job is the aggregate root.
type Job struct {
	id          uuid.UUID
	workspaceID uuid.UUID
	traceID     uuid.UUID
	queue       Queue
	kind        string
	payload     json.RawMessage
	uniqueKey   string
	concurrency string
	status      Status
	attempt     int
	maxAttempts int
	runAfter    time.Time
	leaseOwner  string
	leaseUntil  time.Time
	lastError   string
	createdAt   time.Time
	updatedAt   time.Time
	startedAt   time.Time // the latest attempt's start
	finishedAt  time.Time
}

// New returns a queued job.
func New(id uuid.UUID, spec Spec, now time.Time) (*Job, error) {
	if id == uuid.Nil || spec.WorkspaceID == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "job and workspace ids are required")
	}
	if _, err := ParseQueue(string(spec.Queue)); err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(spec.Kind)
	if kind == "" || len(kind) > MaxKindLength {
		return nil, apperror.New(apperror.ValidationFailed, "job kind is required and at most 100 bytes")
	}
	payload := spec.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	if len(payload) > MaxPayloadBytes || !json.Valid(payload) {
		return nil, apperror.New(apperror.ValidationFailed, "job payload must be JSON up to 1 MiB")
	}
	maxAttempts := spec.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = DefaultMaxAttempts
	}
	if maxAttempts < 1 || maxAttempts > MaxMaxAttempts {
		return nil, apperror.New(apperror.ValidationFailed, "max attempts must be between 1 and 10")
	}
	now = timeutil.Normalize(now)
	runAfter := timeutil.Normalize(spec.RunAfter)
	if runAfter.IsZero() {
		runAfter = now
	}
	trace := spec.TraceID
	if trace == uuid.Nil {
		trace = id
	}
	return &Job{
		id:          id,
		workspaceID: spec.WorkspaceID,
		traceID:     trace,
		queue:       spec.Queue,
		kind:        kind,
		payload:     append(json.RawMessage(nil), payload...),
		uniqueKey:   strings.TrimSpace(spec.UniqueKey),
		concurrency: strings.TrimSpace(spec.ConcurrencyKey),
		status:      StatusQueued,
		maxAttempts: maxAttempts,
		runAfter:    runAfter,
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

// Claimable reports whether a worker may start j at now: it is queued
// and due, or its holder's lease ran out with attempts to spare.
func (j *Job) Claimable(now time.Time) bool {
	switch j.status {
	case StatusQueued:
		return !j.runAfter.After(now)
	case StatusRunning:
		return j.leaseExpired(now) && j.attempt < j.maxAttempts
	}
	return false
}

// Holds reports whether j, running under a live lease at now, holds
// concurrency key: another job with that key must wait for it.
func (j *Job) Holds(key string, now time.Time) bool {
	return key != "" && j.concurrency == key && j.status == StatusRunning && !j.leaseExpired(now)
}

// Abandoned reports whether j's holder vanished after its last attempt,
// so nobody will ever finish it.
func (j *Job) Abandoned(now time.Time) bool {
	return j.status == StatusRunning && j.leaseExpired(now) && j.attempt >= j.maxAttempts
}

func (j *Job) leaseExpired(now time.Time) bool {
	return j.leaseUntil.Before(now)
}

// Start gives owner a lease on j and counts an attempt.
func (j *Job) Start(owner string, lease time.Duration, now time.Time) error {
	if owner == "" || lease <= 0 {
		return apperror.New(apperror.ValidationFailed, "owner and lease are required")
	}
	if !j.Claimable(now) {
		return apperror.New(apperror.InvalidTransition, "job is not claimable")
	}
	now = timeutil.Normalize(now)
	j.status = StatusRunning
	j.attempt++
	j.leaseOwner = owner
	j.leaseUntil = now.Add(lease)
	j.startedAt = now
	j.updatedAt = now
	return nil
}

// Heartbeat extends owner's lease.
func (j *Job) Heartbeat(owner string, lease time.Duration, now time.Time) error {
	if err := j.mustHold(owner); err != nil {
		return err
	}
	now = timeutil.Normalize(now)
	j.leaseUntil = now.Add(lease)
	j.updatedAt = now
	return nil
}

// Succeed finishes j.
func (j *Job) Succeed(owner string, now time.Time) error {
	if err := j.mustHold(owner); err != nil {
		return err
	}
	j.finish(StatusSucceeded, now)
	j.lastError = ""
	return nil
}

// Fail records cause. The job is retried after Backoff unless cause is
// Permanent or the attempts are used up.
func (j *Job) Fail(owner string, cause error, now time.Time) error {
	if err := j.mustHold(owner); err != nil {
		return err
	}
	j.lastError = truncate(cause.Error(), maxErrorLength)
	if errors.Is(cause, ErrPermanent) || j.attempt >= j.maxAttempts {
		j.finish(StatusFailed, now)
		return nil
	}
	now = timeutil.Normalize(now)
	j.status = StatusQueued
	j.runAfter = now.Add(Backoff(j.attempt))
	j.clearLease()
	j.updatedAt = now
	return nil
}

// Release hands j back to the queue without spending the attempt — for
// a worker shutting down mid-job.
func (j *Job) Release(owner string, now time.Time) error {
	if err := j.mustHold(owner); err != nil {
		return err
	}
	now = timeutil.Normalize(now)
	j.status = StatusQueued
	j.attempt--
	j.runAfter = now
	j.clearLease()
	j.updatedAt = now
	return nil
}

// Cancel stops j. A running job's worker learns of it at its next
// heartbeat and abandons the work.
func (j *Job) Cancel(now time.Time) error {
	if j.status.Terminal() {
		return apperror.New(apperror.InvalidTransition, "job has already finished")
	}
	j.finish(StatusCancelled, now)
	return nil
}

// Expire fails an abandoned job.
func (j *Job) Expire(now time.Time) error {
	if !j.Abandoned(now) {
		return apperror.New(apperror.InvalidTransition, "job is not abandoned")
	}
	j.lastError = "worker lease expired on the last attempt"
	j.finish(StatusFailed, now)
	return nil
}

func (j *Job) mustHold(owner string) error {
	if j.status != StatusRunning || j.leaseOwner != owner {
		return apperror.New(apperror.InvalidTransition, "job is not held by this worker")
	}
	return nil
}

func (j *Job) finish(s Status, now time.Time) {
	now = timeutil.Normalize(now)
	j.status = s
	j.clearLease()
	j.finishedAt = now
	j.updatedAt = now
}

func (j *Job) clearLease() {
	j.leaseOwner = ""
	j.leaseUntil = time.Time{}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (j *Job) ID() uuid.UUID            { return j.id }
func (j *Job) WorkspaceID() uuid.UUID   { return j.workspaceID }
func (j *Job) TraceID() uuid.UUID       { return j.traceID }
func (j *Job) Queue() Queue             { return j.queue }
func (j *Job) Kind() string             { return j.kind }
func (j *Job) Payload() json.RawMessage { return append(json.RawMessage(nil), j.payload...) }
func (j *Job) UniqueKey() string        { return j.uniqueKey }
func (j *Job) ConcurrencyKey() string   { return j.concurrency }
func (j *Job) Status() Status           { return j.status }
func (j *Job) Attempt() int             { return j.attempt }
func (j *Job) MaxAttempts() int         { return j.maxAttempts }
func (j *Job) RunAfter() time.Time      { return j.runAfter }
func (j *Job) LeaseOwner() string       { return j.leaseOwner }
func (j *Job) LeaseUntil() time.Time    { return j.leaseUntil }
func (j *Job) LastError() string        { return j.lastError }
func (j *Job) CreatedAt() time.Time     { return j.createdAt }
func (j *Job) UpdatedAt() time.Time     { return j.updatedAt }
func (j *Job) StartedAt() time.Time     { return j.startedAt }
func (j *Job) FinishedAt() time.Time    { return j.finishedAt }

// Snapshot is the persisted form of a Job.
type Snapshot struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	TraceID        uuid.UUID
	Queue          string
	Kind           string
	Payload        json.RawMessage
	UniqueKey      string
	ConcurrencyKey string
	Status         string
	Attempt        int
	MaxAttempts    int
	RunAfter       time.Time
	LeaseOwner     string
	LeaseUntil     time.Time
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      time.Time
	FinishedAt     time.Time
}

// Snapshot returns j's persisted form.
func (j *Job) Snapshot() Snapshot {
	return Snapshot{
		ID:             j.id,
		WorkspaceID:    j.workspaceID,
		TraceID:        j.traceID,
		Queue:          string(j.queue),
		Kind:           j.kind,
		Payload:        j.Payload(),
		UniqueKey:      j.uniqueKey,
		ConcurrencyKey: j.concurrency,
		Status:         string(j.status),
		Attempt:        j.attempt,
		MaxAttempts:    j.maxAttempts,
		RunAfter:       j.runAfter,
		LeaseOwner:     j.leaseOwner,
		LeaseUntil:     j.leaseUntil,
		LastError:      j.lastError,
		CreatedAt:      j.createdAt,
		UpdatedAt:      j.updatedAt,
		StartedAt:      j.startedAt,
		FinishedAt:     j.finishedAt,
	}
}

// Rehydrate rebuilds a Job from storage.
func Rehydrate(s Snapshot) (*Job, error) {
	queue, err := ParseQueue(s.Queue)
	if err != nil {
		return nil, err
	}
	status, err := ParseStatus(s.Status)
	if err != nil {
		return nil, err
	}
	if s.ID == uuid.Nil || s.WorkspaceID == uuid.Nil || s.Kind == "" || s.MaxAttempts < 1 || s.Attempt < 0 {
		return nil, apperror.New(apperror.InternalError, "stored job is invalid")
	}
	if status == StatusRunning && s.LeaseOwner == "" {
		return nil, apperror.New(apperror.InternalError, "stored running job has no lease owner")
	}
	trace := s.TraceID
	if trace == uuid.Nil {
		trace = s.ID
	}
	return &Job{
		id:          s.ID,
		workspaceID: s.WorkspaceID,
		traceID:     trace,
		queue:       queue,
		kind:        s.Kind,
		payload:     append(json.RawMessage(nil), s.Payload...),
		uniqueKey:   s.UniqueKey,
		concurrency: s.ConcurrencyKey,
		status:      status,
		attempt:     s.Attempt,
		maxAttempts: s.MaxAttempts,
		runAfter:    timeutil.Normalize(s.RunAfter),
		leaseOwner:  s.LeaseOwner,
		leaseUntil:  timeutil.Normalize(s.LeaseUntil),
		lastError:   s.LastError,
		createdAt:   timeutil.Normalize(s.CreatedAt),
		updatedAt:   timeutil.Normalize(s.UpdatedAt),
		startedAt:   timeutil.Normalize(s.StartedAt),
		finishedAt:  timeutil.Normalize(s.FinishedAt),
	}, nil
}
