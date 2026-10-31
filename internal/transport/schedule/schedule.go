// Package schedule is the clock-driven transport: it calls a service's
// TriggerDue (due pipeline runs, due site crawls) on a fixed tick, the
// way HTTP calls handlers on requests.
package schedule

import (
	"context"
	"log/slog"
	"time"
)

// Triggerer is the use case the ticker drives.
type Triggerer interface {
	TriggerDue(ctx context.Context) (int, error)
}

// Run calls t.TriggerDue immediately and then every tick until ctx ends.
func Run(ctx context.Context, t Triggerer, tick time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		n, err := t.TriggerDue(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			log.ErrorContext(ctx, "trigger due work", slog.Any("error", err))
		case n > 0:
			log.InfoContext(ctx, "triggered due work", slog.Int("count", n))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
