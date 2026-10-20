package job

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
)

// Handler does the work of one job. Returning an error retries the job
// (unless it wraps jobdomain.ErrPermanent); ctx is cancelled when the job
// is cancelled or the worker shuts down.
type Handler func(ctx context.Context, j *jobdomain.Job) error

// Throttle is how hard a host may work (PLAN.md §4).
type Throttle struct {
	// Concurrency per queue; a missing or zero entry means this host does
	// not serve that queue.
	Concurrency map[jobdomain.Queue]int
	// HeavyExclusive lets only one heavy job run at a time on this host,
	// whatever the per-queue concurrency.
	HeavyExclusive bool
	// Heavy lists the heavy queues; nil means jobdomain.DefaultHeavy
	// (agent, tts, render).
	Heavy []jobdomain.Queue
	// Cooldown is the pause after each heavy job before the next starts.
	Cooldown time.Duration
	// Paused stops claiming new jobs.
	Paused bool
}

func (t Throttle) heavy(q jobdomain.Queue) bool {
	if t.Heavy == nil {
		return q.Heavy()
	}
	return slices.Contains(t.Heavy, q)
}

// MaxSlotsPerQueue bounds per-queue concurrency, so throttle changes at
// runtime can raise it without restarting the worker.
const MaxSlotsPerQueue = 8

// RunnerConfig configures a Runner.
type RunnerConfig struct {
	Owner    string // unique per worker process, e.g. host:pid
	Throttle Throttle
	// ThrottleSource, when set, is asked before every claim and overrides
	// Throttle, so settings edited in the UI apply without a restart.
	ThrottleSource func(ctx context.Context) Throttle
	// ThrottleChanged, when set, returns a channel closed at the next
	// throttle change: idle, cooling-down and heavy-waiting slots then
	// re-read the throttle at once instead of finishing their wait.
	ThrottleChanged func() <-chan struct{}
	Lease           time.Duration // default 2m
	Poll            time.Duration // idle wait between claims; default 2s
	SweepEach       time.Duration // abandoned-job sweep period; default Lease
}

// Queue is where a runner gets jobs: the job service in process, or the
// server's worker API for a remote (hybrid) worker.
type Queue interface {
	Claim(ctx context.Context, queues []jobdomain.Queue, kinds []string, owner string, lease time.Duration) (*jobdomain.Job, error)
	Heartbeat(ctx context.Context, jobID uuid.UUID, owner string, lease time.Duration) error
	Complete(ctx context.Context, jobID uuid.UUID, owner string, cause error) error
	Release(ctx context.Context, jobID uuid.UUID, owner string) error
	ExpireAbandoned(ctx context.Context) (int, error)
}

var _ Queue = (*Service)(nil)

// Runner claims and executes jobs.
type Runner struct {
	svc      Queue
	cfg      RunnerConfig
	handlers map[string]Handler
	heavy    chan struct{}
	log      *slog.Logger
}

// NewRunner returns a Runner. Register handlers before Run.
func NewRunner(svc Queue, cfg RunnerConfig, log *slog.Logger) *Runner {
	if cfg.Lease <= 0 {
		cfg.Lease = 2 * time.Minute
	}
	if cfg.Poll <= 0 {
		cfg.Poll = 2 * time.Second
	}
	if cfg.SweepEach <= 0 {
		cfg.SweepEach = cfg.Lease
	}
	return &Runner{
		svc:      svc,
		cfg:      cfg,
		handlers: map[string]Handler{},
		heavy:    make(chan struct{}, 1),
		log:      log,
	}
}

// Register binds kind to h. Jobs of unregistered kinds are left for a
// worker that has them.
func (r *Runner) Register(kind string, h Handler) {
	r.handlers[kind] = h
}

// Run blocks until ctx is done.
func (r *Runner) Run(ctx context.Context) error {
	kinds := slices.Sorted(maps.Keys(r.handlers))
	slots := func(q jobdomain.Queue) int {
		if r.cfg.ThrottleSource != nil {
			return MaxSlotsPerQueue
		}
		return min(r.cfg.Throttle.Concurrency[q], MaxSlotsPerQueue)
	}
	var wg sync.WaitGroup
	for _, q := range jobdomain.Queues {
		for slot := range slots(q) {
			wg.Go(func() { r.loop(ctx, q, slot, kinds) })
		}
	}
	wg.Go(func() { r.sweep(ctx) })
	wg.Wait()
	return nil
}

func (r *Runner) throttle(ctx context.Context) Throttle {
	if r.cfg.ThrottleSource != nil {
		return r.cfg.ThrottleSource(ctx)
	}
	return r.cfg.Throttle
}

// changed returns the channel closed at the next throttle change; nil
// (never ready) without a ThrottleChanged.
func (r *Runner) changed() <-chan struct{} {
	if r.cfg.ThrottleChanged == nil {
		return nil
	}
	return r.cfg.ThrottleChanged()
}

func (r *Runner) loop(ctx context.Context, q jobdomain.Queue, slot int, kinds []string) {
	for ctx.Err() == nil {
		changed := r.changed() // before the read, so no change slips between
		t := r.throttle(ctx)
		if t.Paused || slot >= t.Concurrency[q] {
			wait(ctx, r.cfg.Poll, changed)
			continue
		}
		exclusive := t.HeavyExclusive && t.heavy(q)
		if exclusive {
			select {
			case r.heavy <- struct{}{}:
			case <-changed:
				continue // exclusivity, the queue or a pause may have changed
			case <-ctx.Done():
				return
			}
		}
		ran := r.claimAndRun(ctx, q, kinds)
		if exclusive {
			if ran {
				r.cooldown(ctx, time.Now())
			}
			<-r.heavy
		}
		if !ran {
			wait(ctx, r.cfg.Poll, r.changed())
		}
	}
}

// cooldown rests after a heavy job that ended at since. A throttle
// change shortens or lengthens the rest at once; turning exclusivity off
// ends it.
func (r *Runner) cooldown(ctx context.Context, since time.Time) {
	for ctx.Err() == nil {
		changed := r.changed()
		t := r.throttle(ctx)
		left := t.Cooldown - time.Since(since)
		if left <= 0 || !t.HeavyExclusive {
			return
		}
		wait(ctx, left, changed)
	}
}

// claimAndRun runs at most one job and reports whether it did.
func (r *Runner) claimAndRun(ctx context.Context, q jobdomain.Queue, kinds []string) bool {
	j, err := r.svc.Claim(ctx, []jobdomain.Queue{q}, kinds, r.cfg.Owner, r.cfg.Lease)
	if err != nil {
		if ctx.Err() == nil {
			r.log.ErrorContext(ctx, "claim job", slog.String("queue", string(q)), slog.Any("error", err))
		}
		return false
	}
	if j == nil {
		return false
	}
	r.execute(ctx, j)
	return true
}

func (r *Runner) execute(ctx context.Context, j *jobdomain.Job) {
	log := r.log.With(slog.String("job_id", j.ID().String()), slog.String("kind", j.Kind()), slog.Int("attempt", j.Attempt()))
	jobCtx, cancel := context.WithCancel(jobdomain.WithTrace(ctx, j.TraceID()))
	defer cancel()

	lost := make(chan struct{})
	go r.heartbeat(jobCtx, j, cancel, lost, log)

	started := time.Now()
	err := r.run(jobCtx, j)
	cancel()

	// Use a fresh context: the parent may be shutting down, and the
	// outcome still has to be written.
	finishCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()
	select {
	case <-lost:
		log.WarnContext(finishCtx, "job lease lost or job cancelled; outcome discarded")
		return
	default:
	}
	if ctx.Err() != nil && err != nil {
		if rerr := r.svc.Release(finishCtx, j.ID(), r.cfg.Owner); rerr != nil {
			log.ErrorContext(finishCtx, "release job on shutdown", slog.Any("error", rerr))
		}
		return
	}
	if cerr := r.svc.Complete(finishCtx, j.ID(), r.cfg.Owner, err); cerr != nil {
		log.ErrorContext(finishCtx, "record job outcome", slog.Any("error", cerr))
		return
	}
	if err != nil {
		log.WarnContext(finishCtx, "job failed", slog.Any("error", err), slog.Duration("took", time.Since(started)))
		return
	}
	log.InfoContext(finishCtx, "job succeeded", slog.Duration("took", time.Since(started)))
}

func (r *Runner) run(ctx context.Context, j *jobdomain.Job) (err error) {
	h, ok := r.handlers[j.Kind()]
	if !ok {
		return jobdomain.Permanent(fmt.Errorf("no handler for kind %q", j.Kind()))
	}
	defer func() {
		if p := recover(); p != nil {
			err = jobdomain.Permanent(fmt.Errorf("handler panicked: %v", p))
		}
	}()
	return h(ctx, j)
}

// heartbeat extends the lease every third of it. When the job is
// cancelled or another worker took it over, it cancels the handler and
// closes lost. A heartbeat that fails for another reason (a busy
// database, the server out of reach) is tried again at the next tick
// while the lease it last extended holds; only once that lease ran out
// may another worker have taken the job, so only then is the job given up.
func (r *Runner) heartbeat(ctx context.Context, j *jobdomain.Job, cancel context.CancelFunc, lost chan<- struct{}, log *slog.Logger) {
	t := time.NewTicker(r.cfg.Lease / 3)
	defer t.Stop()
	held := time.Now().Add(r.cfg.Lease) // the lease the claim gave
	giveUp := func() {
		close(lost)
		cancel()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sent := time.Now()
			err := r.svc.Heartbeat(ctx, j.ID(), r.cfg.Owner, r.cfg.Lease)
			switch {
			case err == nil:
				held = sent.Add(r.cfg.Lease)
			case errors.Is(err, context.Canceled):
			case leaseGone(err):
				log.WarnContext(ctx, "heartbeat refused; stopping job", slog.Any("error", err))
				giveUp()
				return
			case time.Now().Before(held):
				log.WarnContext(ctx, "heartbeat failed; trying again", slog.Any("error", err))
			default:
				log.WarnContext(ctx, "lease ran out without a heartbeat; stopping job", slog.Any("error", err))
				giveUp()
				return
			}
		}
	}
}

// leaseGone reports whether a heartbeat error means the job is no longer
// this worker's: it was cancelled, finished or taken over, or is gone.
func leaseGone(err error) bool {
	return apperror.Is(err, apperror.InvalidTransition) || apperror.Is(err, apperror.JobNotFound) ||
		apperror.Is(err, apperror.Conflict)
}

func (r *Runner) sweep(ctx context.Context) {
	t := time.NewTicker(r.cfg.SweepEach)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := r.svc.ExpireAbandoned(ctx)
			if err != nil && ctx.Err() == nil {
				r.log.ErrorContext(ctx, "expire abandoned jobs", slog.Any("error", err))
			} else if n > 0 {
				r.log.WarnContext(ctx, "expired abandoned jobs", slog.Int("count", n))
			}
		}
	}
}

// wait returns after d, when ctx ends, or when changed closes.
func wait(ctx context.Context, d time.Duration, changed <-chan struct{}) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	case <-changed:
	}
}
