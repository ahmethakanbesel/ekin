package identity

import (
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// Membership puts a user in a workspace with a role. A workspace always
// keeps at least one owner; the owner count is passed in because it spans
// memberships.
type Membership struct {
	workspaceID uuid.UUID
	userID      uuid.UUID
	role        Role
	createdAt   time.Time
	updatedAt   time.Time
}

// NewMembership returns a membership.
func NewMembership(workspaceID, userID uuid.UUID, role Role, now time.Time) (*Membership, error) {
	if workspaceID == uuid.Nil || userID == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "workspace and user are required")
	}
	if _, err := ParseRole(string(role)); err != nil {
		return nil, err
	}
	now = timeutil.Normalize(now)
	return &Membership{workspaceID: workspaceID, userID: userID, role: role, createdAt: now, updatedAt: now}, nil
}

// Can reports whether the member may perform a.
func (m *Membership) Can(a Action) bool { return m.role.Can(a) }

// Authorize returns Forbidden unless the member may perform a.
func (m *Membership) Authorize(a Action) error {
	if m.Can(a) {
		return nil
	}
	return apperror.New(apperror.Forbidden, "your role ("+string(m.role)+") cannot "+a.String()+" this workspace")
}

// ChangeRole sets m's role on behalf of by. owners is the workspace's
// current owner count.
func (m *Membership) ChangeRole(by *Membership, role Role, owners int, now time.Time) error {
	if err := m.sameWorkspace(by); err != nil {
		return err
	}
	if err := by.Authorize(ActionManage); err != nil {
		return err
	}
	if _, err := ParseRole(string(role)); err != nil {
		return err
	}
	if m.role == RoleOwner && role != RoleOwner && owners <= 1 {
		return apperror.New(apperror.LastOwner, "the workspace needs another owner first")
	}
	m.role = role
	m.updatedAt = timeutil.Normalize(now)
	return nil
}

// CheckRemoval allows removing m on behalf of by: owners remove anyone,
// and everyone may leave. The last owner may not go.
func (m *Membership) CheckRemoval(by *Membership, owners int) error {
	if err := m.sameWorkspace(by); err != nil {
		return err
	}
	if by.userID != m.userID {
		if err := by.Authorize(ActionManage); err != nil {
			return err
		}
	}
	if m.role == RoleOwner && owners <= 1 {
		return apperror.New(apperror.LastOwner, "the workspace needs another owner first")
	}
	return nil
}

func (m *Membership) sameWorkspace(by *Membership) error {
	if by == nil || by.workspaceID != m.workspaceID {
		return apperror.New(apperror.Forbidden, "not a member of this workspace")
	}
	return nil
}

func (m *Membership) WorkspaceID() uuid.UUID { return m.workspaceID }
func (m *Membership) UserID() uuid.UUID      { return m.userID }
func (m *Membership) Role() Role             { return m.role }
func (m *Membership) CreatedAt() time.Time   { return m.createdAt }
func (m *Membership) UpdatedAt() time.Time   { return m.updatedAt }

// MembershipSnapshot is the persisted form of a Membership.
type MembershipSnapshot struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Role        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Snapshot returns m's persisted form.
func (m *Membership) Snapshot() MembershipSnapshot {
	return MembershipSnapshot{
		WorkspaceID: m.workspaceID,
		UserID:      m.userID,
		Role:        string(m.role),
		CreatedAt:   m.createdAt,
		UpdatedAt:   m.updatedAt,
	}
}

// RehydrateMembership rebuilds a Membership from storage.
func RehydrateMembership(s MembershipSnapshot) (*Membership, error) {
	role, err := ParseRole(s.Role)
	if err != nil {
		return nil, err
	}
	m, err := NewMembership(s.WorkspaceID, s.UserID, role, s.CreatedAt)
	if err != nil {
		return nil, err
	}
	m.updatedAt = timeutil.Normalize(s.UpdatedAt)
	return m, nil
}
