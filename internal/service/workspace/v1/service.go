// Package workspace orchestrates workspace use cases.
package workspace

import (
	"context"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	identitydomain "kurgu/internal/domain/identity/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	identityrepo "kurgu/internal/repository/identity/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
)

// Service runs workspace use cases.
type Service struct {
	repo        workspacerepo.Repository
	memberships identityrepo.Memberships
	tx          db.TxManager
	now         func() time.Time
}

// NewService returns a Service.
func NewService(repo workspacerepo.Repository, memberships identityrepo.Memberships, tx db.TxManager) *Service {
	return &Service{repo: repo, memberships: memberships, tx: tx, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Create creates a workspace owned by ownerID.
func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name string, loc *time.Location) (*workspacedomain.Workspace, error) {
	now := s.now()
	w, err := workspacedomain.New(id.New(), name, loc, now)
	if err != nil {
		return nil, err
	}
	owner, err := identitydomain.NewMembership(w.ID(), ownerID, identitydomain.RoleOwner, now)
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, w); err != nil {
			return err
		}
		return s.memberships.CreateMembership(ctx, owner)
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// Get returns one workspace.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*workspacedomain.Workspace, error) {
	return s.repo.Get(ctx, id)
}

// Entry is a workspace with the caller's role in it.
type Entry struct {
	Workspace *workspacedomain.Workspace
	Role      identitydomain.Role
}

// ListFor returns the workspaces userID belongs to, oldest membership
// first.
func (s *Service) ListFor(ctx context.Context, userID uuid.UUID) ([]Entry, error) {
	ms, err := s.memberships.ListUserMemberships(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(ms))
	for _, m := range ms {
		w, err := s.repo.Get(ctx, m.WorkspaceID())
		if err != nil {
			return nil, err
		}
		out = append(out, Entry{Workspace: w, Role: m.Role()})
	}
	return out, nil
}

// AdoptOrphans makes userID the owner of every workspace without members
// (workspaces created before accounts existed) and returns how many.
func (s *Service) AdoptOrphans(ctx context.Context, userID uuid.UUID) (int, error) {
	adopted := 0
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		all, err := s.repo.List(ctx)
		if err != nil {
			return err
		}
		for _, w := range all {
			n, err := s.memberships.CountMembers(ctx, w.ID())
			if err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			m, err := identitydomain.NewMembership(w.ID(), userID, identitydomain.RoleOwner, s.now())
			if err != nil {
				return err
			}
			if err := s.memberships.CreateMembership(ctx, m); err != nil && !apperror.Is(err, apperror.AlreadyMember) {
				return err
			}
			adopted++
		}
		return nil
	})
	return adopted, err
}

// Change is a parsed partial update; nil fields are left unchanged.
type Change struct {
	Name     *string
	Location *time.Location
}

// Update applies change to the workspace. It reads and saves in one
// transaction, so two people changing different fields at once both keep
// theirs.
func (s *Service) Update(ctx context.Context, id uuid.UUID, change Change) (*workspacedomain.Workspace, error) {
	var out *workspacedomain.Workspace
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		w, err := s.repo.Get(ctx, id)
		if err != nil {
			return err
		}
		now := s.now()
		if change.Name != nil {
			if err := w.Rename(*change.Name, now); err != nil {
				return err
			}
		}
		if change.Location != nil {
			if err := w.ChangeTimezone(change.Location, now); err != nil {
				return err
			}
		}
		out = w
		return s.repo.Update(ctx, w)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
