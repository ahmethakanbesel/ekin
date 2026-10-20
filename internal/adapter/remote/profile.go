package remote

import (
	"context"
	"log/slog"
	"reflect"
	"sync"
	"time"

	settingsdomain "github.com/ahmethakanbesel/ekin/internal/domain/settings/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	"github.com/ahmethakanbesel/ekin/pkg/broadcast"
)

// ProfileWatcher keeps a remote worker's host profile in step with the
// one admins save on the server, so throttling set in the UI reaches
// this machine within one poll. Until the server has a saved profile of
// that name, or while it cannot be reached, the local one applies.
type ProfileWatcher struct {
	client *Client
	name   string
	local  settingsdomain.Profile // from this machine's environment
	every  time.Duration
	log    *slog.Logger

	mu      sync.Mutex
	current settingsdomain.Profile
	failing bool
	changed broadcast.Signal
}

// NewProfileWatcher watches the profile named like local, polling every
// interval (3s when zero).
func NewProfileWatcher(c *Client, local settingsdomain.Profile, every time.Duration, log *slog.Logger) *ProfileWatcher {
	if every <= 0 {
		every = 3 * time.Second
	}
	return &ProfileWatcher{client: c, name: local.Name, local: local, every: every, log: log, current: local}
}

// Run polls until ctx ends.
func (w *ProfileWatcher) Run(ctx context.Context) {
	t := time.NewTicker(w.every)
	defer t.Stop()
	for {
		w.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Refresh fetches the profile once and signals a change.
func (w *ProfileWatcher) Refresh(ctx context.Context) {
	p, ok, err := w.client.Profile(ctx, w.name)
	w.mu.Lock()
	if err != nil {
		if !w.failing && ctx.Err() == nil {
			w.log.WarnContext(ctx, "fetch host profile; keeping the last one", slog.String("profile", w.name), slog.Any("error", err))
		}
		w.failing = true
		w.mu.Unlock()
		return
	}
	w.failing = false
	if !ok {
		p = w.local
	}
	same := reflect.DeepEqual(p, w.current)
	w.current = p
	w.mu.Unlock()
	if !same {
		w.log.InfoContext(ctx, "host profile changed", slog.String("profile", w.name), slog.Bool("saved", ok))
		w.changed.Notify()
	}
}

// Profile returns the profile in force.
func (w *ProfileWatcher) Profile(context.Context) settingsdomain.Profile {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current
}

// Throttle adapts the profile to the job runner.
func (w *ProfileWatcher) Throttle(ctx context.Context) jobsvc.Throttle {
	p := w.Profile(ctx)
	return jobsvc.Throttle{Concurrency: p.Concurrency, HeavyExclusive: p.HeavyExclusive, Heavy: p.HeavyQueues, Cooldown: p.Cooldown, Paused: p.Paused}
}

// Changed returns a channel closed at the next change.
func (w *ProfileWatcher) Changed() <-chan struct{} { return w.changed.Wait() }
