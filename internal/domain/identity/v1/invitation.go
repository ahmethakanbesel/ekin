package identity

import (
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/pkg/timeutil"
)

// InvitationTTL is how long an invitation link works.
const InvitationTTL = 7 * 24 * time.Hour

// Invitation offers an email address a role in a workspace. Whoever holds
// the link and can prove the address's account (or create it) joins.
type Invitation struct {
	id          uuid.UUID
	workspaceID uuid.UUID
	email       Email
	role        Role
	invitedBy   uuid.UUID // Nil once the inviter's account is gone
	tokenHash   string
	createdAt   time.Time
	expiresAt   time.Time
}

// NewInvitation invites email on behalf of by and returns the link
// token, which is not stored.
func NewInvitation(id uuid.UUID, by *Membership, email Email, role Role, now time.Time) (*Invitation, string, error) {
	if id == uuid.Nil {
		return nil, "", apperror.New(apperror.ValidationFailed, "invitation id is required")
	}
	if by == nil {
		return nil, "", apperror.New(apperror.Forbidden, "not a member of this workspace")
	}
	if err := by.Authorize(ActionManage); err != nil {
		return nil, "", err
	}
	if email == "" {
		return nil, "", apperror.New(apperror.ValidationFailed, "email is required")
	}
	if _, err := ParseRole(string(role)); err != nil {
		return nil, "", err
	}
	token, hash := newToken()
	now = timeutil.Normalize(now)
	return &Invitation{
		id:          id,
		workspaceID: by.workspaceID,
		email:       email,
		role:        role,
		invitedBy:   by.userID,
		tokenHash:   hash,
		createdAt:   now,
		expiresAt:   now.Add(InvitationTTL),
	}, token, nil
}

// Pending reports whether the link still works at now.
func (i *Invitation) Pending(now time.Time) bool { return now.Before(i.expiresAt) }

// Accept turns the invitation into u's membership. u must own the invited
// address.
func (i *Invitation) Accept(u *User, now time.Time) (*Membership, error) {
	if !i.Pending(now) {
		return nil, apperror.New(apperror.InvitationNotFound, "invitation has expired")
	}
	if u.Email() != i.email {
		return nil, apperror.New(apperror.Forbidden, "this invitation is for a different email address")
	}
	return NewMembership(i.workspaceID, u.ID(), i.role, now)
}

func (i *Invitation) ID() uuid.UUID          { return i.id }
func (i *Invitation) WorkspaceID() uuid.UUID { return i.workspaceID }
func (i *Invitation) Email() Email           { return i.email }
func (i *Invitation) Role() Role             { return i.role }
func (i *Invitation) InvitedBy() uuid.UUID   { return i.invitedBy }
func (i *Invitation) TokenHash() string      { return i.tokenHash }
func (i *Invitation) CreatedAt() time.Time   { return i.createdAt }
func (i *Invitation) ExpiresAt() time.Time   { return i.expiresAt }

// InvitationSnapshot is the persisted form of an Invitation.
type InvitationSnapshot struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Email       string
	Role        string
	InvitedBy   uuid.UUID
	TokenHash   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Snapshot returns i's persisted form.
func (i *Invitation) Snapshot() InvitationSnapshot {
	return InvitationSnapshot{
		ID:          i.id,
		WorkspaceID: i.workspaceID,
		Email:       string(i.email),
		Role:        string(i.role),
		InvitedBy:   i.invitedBy,
		TokenHash:   i.tokenHash,
		CreatedAt:   i.createdAt,
		ExpiresAt:   i.expiresAt,
	}
}

// RehydrateInvitation rebuilds an Invitation from storage.
func RehydrateInvitation(s InvitationSnapshot) (*Invitation, error) {
	email, err := ParseEmail(s.Email)
	if err != nil {
		return nil, err
	}
	role, err := ParseRole(s.Role)
	if err != nil {
		return nil, err
	}
	if s.ID == uuid.Nil || s.WorkspaceID == uuid.Nil || s.TokenHash == "" {
		return nil, apperror.New(apperror.InternalError, "stored invitation is incomplete")
	}
	return &Invitation{
		id:          s.ID,
		workspaceID: s.WorkspaceID,
		email:       email,
		role:        role,
		invitedBy:   s.InvitedBy,
		tokenHash:   s.TokenHash,
		createdAt:   timeutil.Normalize(s.CreatedAt),
		expiresAt:   timeutil.Normalize(s.ExpiresAt),
	}, nil
}
