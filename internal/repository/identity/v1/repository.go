// Package identity persists users, sessions, memberships and
// invitations.
package identity

import (
	"context"
	"time"

	"github.com/google/uuid"

	identitydomain "ekin/internal/domain/identity/v1"
)

// Repository persists the identity aggregates. One implementation backs
// all four so membership listings can join users.
type Repository interface {
	Users
	Sessions
	Memberships
	Invitations
}

// Users persists User aggregates.
type Users interface {
	// CreateUser returns EmailTaken when the address has an account.
	CreateUser(ctx context.Context, u *identitydomain.User) error
	UpdateUser(ctx context.Context, u *identitydomain.User) error
	// GetUser returns UserNotFound when id does not exist.
	GetUser(ctx context.Context, id uuid.UUID) (*identitydomain.User, error)
	// GetUserByEmail returns UserNotFound when no account has email.
	GetUserByEmail(ctx context.Context, email identitydomain.Email) (*identitydomain.User, error)
	CountUsers(ctx context.Context) (int, error)
}

// Sessions persists Session aggregates.
type Sessions interface {
	CreateSession(ctx context.Context, s *identitydomain.Session) error
	// GetSession returns Unauthenticated when id does not exist.
	GetSession(ctx context.Context, id string) (*identitydomain.Session, error)
	UpdateSession(ctx context.Context, s *identitydomain.Session) error
	DeleteSession(ctx context.Context, id string) error
	// DeleteUserSessions signs userID out everywhere except keep ("" keeps
	// none).
	DeleteUserSessions(ctx context.Context, userID uuid.UUID, keep string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error)
}

// Member is a membership joined with its user, for listings.
type Member struct {
	Membership *identitydomain.Membership
	User       *identitydomain.User
}

// Memberships persists Membership aggregates.
type Memberships interface {
	// CreateMembership returns AlreadyMember when the user is in the
	// workspace.
	CreateMembership(ctx context.Context, m *identitydomain.Membership) error
	UpdateMembership(ctx context.Context, m *identitydomain.Membership) error
	// GetMembership returns MemberNotFound when the user is not in the
	// workspace.
	GetMembership(ctx context.Context, workspaceID, userID uuid.UUID) (*identitydomain.Membership, error)
	DeleteMembership(ctx context.Context, workspaceID, userID uuid.UUID) error
	// ListMembers returns owners first, then by join time.
	ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]Member, error)
	// ListUserMemberships returns the user's memberships, oldest first.
	ListUserMemberships(ctx context.Context, userID uuid.UUID) ([]*identitydomain.Membership, error)
	CountOwners(ctx context.Context, workspaceID uuid.UUID) (int, error)
	CountMembers(ctx context.Context, workspaceID uuid.UUID) (int, error)
}

// Invitations persists Invitation aggregates.
type Invitations interface {
	// CreateInvitation replaces any invitation for the same workspace and
	// email.
	CreateInvitation(ctx context.Context, i *identitydomain.Invitation) error
	// GetInvitationByToken returns InvitationNotFound for an unknown hash.
	GetInvitationByToken(ctx context.Context, tokenHash string) (*identitydomain.Invitation, error)
	// ListInvitations returns the workspace's invitations still pending at
	// now, newest first.
	ListInvitations(ctx context.Context, workspaceID uuid.UUID, now time.Time) ([]*identitydomain.Invitation, error)
	// DeleteInvitation returns InvitationNotFound when it does not exist.
	DeleteInvitation(ctx context.Context, workspaceID, id uuid.UUID) error
	DeleteExpiredInvitations(ctx context.Context, now time.Time) (int, error)
}
