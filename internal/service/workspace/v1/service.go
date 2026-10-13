// Package workspace orchestrates workspace use cases.
package workspace

import (
	"context"
	"time"

	"github.com/google/uuid"

	workspacedomain "kurgu/internal/domain/workspace/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
)

// Service runs workspace use cases.
type Service struct {
	repo workspacerepo.Repository
	now  func() time.Time
}

// NewService returns a Service over repo.
func NewService(repo workspacerepo.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Create creates a workspace.
func (s *Service) Create(ctx context.Context, name string, loc *time.Location) (*workspacedomain.Workspace, error) {
	w, err := workspacedomain.New(id.New(), name, loc, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

// Get returns one workspace.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*workspacedomain.Workspace, error) {
	return s.repo.Get(ctx, id)
}

// List returns every workspace.
func (s *Service) List(ctx context.Context) ([]*workspacedomain.Workspace, error) {
	return s.repo.List(ctx)
}

// Change is a parsed partial update; nil fields are left unchanged.
type Change struct {
	Name     *string
	Location *time.Location
}

// Update applies change to the workspace.
func (s *Service) Update(ctx context.Context, id uuid.UUID, change Change) (*workspacedomain.Workspace, error) {
	w, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if change.Name != nil {
		if err := w.Rename(*change.Name, now); err != nil {
			return nil, err
		}
	}
	if change.Location != nil {
		if err := w.ChangeTimezone(change.Location, now); err != nil {
			return nil, err
		}
	}
	if err := s.repo.Update(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}
