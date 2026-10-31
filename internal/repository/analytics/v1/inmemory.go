package analytics

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	analyticsdomain "github.com/ahmethakanbesel/ekin/internal/domain/analytics/v1"
)

// InMemory is a Repository for tests.
type InMemory struct {
	mu      sync.RWMutex
	conns   map[uuid.UUID]analyticsdomain.Snapshot
	metrics map[uuid.UUID]map[analyticsdomain.Dimension][]analyticsdomain.Metric
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory.
func NewInMemory() *InMemory {
	return &InMemory{conns: map[uuid.UUID]analyticsdomain.Snapshot{}, metrics: map[uuid.UUID]map[analyticsdomain.Dimension][]analyticsdomain.Metric{}}
}

func (r *InMemory) Connection(_ context.Context, ws uuid.UUID) (*analyticsdomain.Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sn, ok := r.conns[ws]
	if !ok {
		return nil, apperror.New(apperror.NotFound, "search console is not connected")
	}
	return analyticsdomain.Rehydrate(sn)
}

func (r *InMemory) Connections(_ context.Context) ([]*analyticsdomain.Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*analyticsdomain.Connection
	for _, sn := range r.conns {
		if sn.Property == "" {
			continue
		}
		c, err := analyticsdomain.Rehydrate(sn)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b *analyticsdomain.Connection) int {
		return strings.Compare(a.WorkspaceID().String(), b.WorkspaceID().String())
	})
	return out, nil
}

func (r *InMemory) PutConnection(_ context.Context, c *analyticsdomain.Connection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns[c.WorkspaceID()] = c.Snapshot()
	return nil
}

func (r *InMemory) DeleteConnection(_ context.Context, ws uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.conns, ws)
	return nil
}

func inRange(d, start, end time.Time) bool {
	d = analyticsdomain.Day(d)
	return !d.Before(analyticsdomain.Day(start)) && !d.After(analyticsdomain.Day(end))
}

func (r *InMemory) ReplaceMetrics(_ context.Context, ws uuid.UUID, dim analyticsdomain.Dimension, start, end time.Time, ms []analyticsdomain.Metric) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.metrics[ws] == nil {
		r.metrics[ws] = map[analyticsdomain.Dimension][]analyticsdomain.Metric{}
	}
	kept := slices.DeleteFunc(slices.Clone(r.metrics[ws][dim]), func(m analyticsdomain.Metric) bool { return inRange(m.Date, start, end) })
	for _, m := range ms {
		if inRange(m.Date, start, end) {
			m.Date = analyticsdomain.Day(m.Date)
			kept = slices.DeleteFunc(kept, func(x analyticsdomain.Metric) bool { return x.Date.Equal(m.Date) && x.Key == m.Key })
			kept = append(kept, m)
		}
	}
	r.metrics[ws][dim] = kept
	return nil
}

func (r *InMemory) Metrics(_ context.Context, ws uuid.UUID, dim analyticsdomain.Dimension, keys []string, start, end time.Time) ([]analyticsdomain.Metric, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []analyticsdomain.Metric
	for _, m := range r.metrics[ws][dim] {
		if inRange(m.Date, start, end) && (len(keys) == 0 || slices.Contains(keys, m.Key)) {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b analyticsdomain.Metric) int {
		return cmp.Or(a.Date.Compare(b.Date), strings.Compare(a.Key, b.Key))
	})
	return out, nil
}

func (r *InMemory) Top(ctx context.Context, ws uuid.UUID, dim analyticsdomain.Dimension, start, end time.Time, limit int) ([]KeyTotals, error) {
	ms, _ := r.Metrics(ctx, ws, dim, nil, start, end)
	byKey := map[string][]analyticsdomain.Metric{}
	for _, m := range ms {
		byKey[m.Key] = append(byKey[m.Key], m)
	}
	out := make([]KeyTotals, 0, len(byKey))
	for k, list := range byKey {
		out = append(out, KeyTotals{Key: k, Totals: analyticsdomain.Sum(list, start, end)})
	}
	slices.SortFunc(out, func(a, b KeyTotals) int {
		return cmp.Or(cmp.Compare(b.Clicks, a.Clicks), cmp.Compare(b.Impressions, a.Impressions), strings.Compare(a.Key, b.Key))
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *InMemory) ClearMetrics(_ context.Context, ws uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.metrics, ws)
	return nil
}
