// Package identity runs sign-in, account, membership and invitation use
// cases, and answers whether a user may act in a workspace.
package identity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/internal/db"
	identitydomain "kurgu/internal/domain/identity/v1"
	identityrepo "kurgu/internal/repository/identity/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	"kurgu/pkg/id"
)

const (
	// maxFailures wrong passwords per email within failureWindow lock that
	// email out for the rest of the window.
	maxFailures   = 10
	failureWindow = 15 * time.Minute
)

// Hasher hashes and verifies passwords (pkg/password).
type Hasher interface {
	Hash(plain string) (string, error)
	Verify(encoded, plain string) (bool, error)
}

// Member is a membership with its user.
type Member = identityrepo.Member

// SignIn is a new session and the token for its cookie.
type SignIn struct {
	User    *identitydomain.User
	Session *identitydomain.Session
	Token   string
}

// Service runs identity use cases.
type Service struct {
	repo       identityrepo.Repository
	workspaces workspacerepo.Repository
	tx         db.TxManager
	hasher     Hasher
	limiter    *limiter
	now        func() time.Time

	dummyOnce sync.Once
	dummyHash string
}

// NewService returns a Service.
func NewService(repo identityrepo.Repository, workspaces workspacerepo.Repository, tx db.TxManager, hasher Hasher) *Service {
	return &Service{
		repo:       repo,
		workspaces: workspaces,
		tx:         tx,
		hasher:     hasher,
		limiter:    newLimiter(maxFailures, failureWindow),
		now:        time.Now,
	}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// CreateUser creates an account directly (the CLI's bootstrap path).
func (s *Service) CreateUser(ctx context.Context, email identitydomain.Email, name string, pw identitydomain.Password) (*identitydomain.User, error) {
	hash, err := s.hasher.Hash(pw.Plain())
	if err != nil {
		return nil, err
	}
	u, err := identitydomain.NewUser(id.New(), email, name, hash, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// ResetPassword sets a password without the old one and signs the user
// out everywhere (the CLI's recovery path).
func (s *Service) ResetPassword(ctx context.Context, email identitydomain.Email, pw identitydomain.Password) error {
	hash, err := s.hasher.Hash(pw.Plain())
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		u, err := s.repo.GetUserByEmail(ctx, email)
		if err != nil {
			return err
		}
		if err := u.SetPasswordHash(hash, s.now()); err != nil {
			return err
		}
		if err := s.repo.UpdateUser(ctx, u); err != nil {
			return err
		}
		return s.repo.DeleteUserSessions(ctx, u.ID(), "")
	})
}

// Login checks email and password and starts a session. Unknown emails
// and wrong passwords fail the same way, in about the same time.
func (s *Service) Login(ctx context.Context, email identitydomain.Email, password string) (SignIn, error) {
	now := s.now()
	key := string(email)
	if s.limiter.blocked(key, now) {
		return SignIn{}, apperror.New(apperror.TooManyAttempts, "too many failed attempts; try again in 15 minutes")
	}
	u, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil && !apperror.Is(err, apperror.UserNotFound) {
		return SignIn{}, err
	}
	if u == nil {
		_, _ = s.hasher.Verify(s.dummy(), password)
		s.limiter.fail(key, now)
		return SignIn{}, errBadCredentials
	}
	if err := s.checkPassword(u, password, now); err != nil {
		return SignIn{}, err
	}
	return s.startSession(ctx, u, now)
}

var errBadCredentials = apperror.New(apperror.Unauthenticated, "wrong email or password")

// checkPassword verifies password against u, counting failures.
func (s *Service) checkPassword(u *identitydomain.User, password string, now time.Time) error {
	key := string(u.Email())
	if s.limiter.blocked(key, now) {
		return apperror.New(apperror.TooManyAttempts, "too many failed attempts; try again in 15 minutes")
	}
	ok, err := s.hasher.Verify(u.PasswordHash(), password)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		s.limiter.fail(key, now)
		return errBadCredentials
	}
	s.limiter.reset(key)
	return nil
}

func (s *Service) startSession(ctx context.Context, u *identitydomain.User, now time.Time) (SignIn, error) {
	sess, token := identitydomain.NewSession(u.ID(), now)
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return SignIn{}, err
	}
	return SignIn{User: u, Session: sess, Token: token}, nil
}

// dummy is a hash to verify against when the email is unknown, so the
// response time does not reveal which emails have accounts.
func (s *Service) dummy() string {
	s.dummyOnce.Do(func() {
		s.dummyHash, _ = s.hasher.Hash("kurgu-timing-equaliser")
	})
	return s.dummyHash
}

// Authenticate resolves a session token to its user, sliding the
// session's idle expiry at most once an hour.
func (s *Service) Authenticate(ctx context.Context, token string) (*identitydomain.User, *identitydomain.Session, error) {
	if token == "" {
		return nil, nil, apperror.New(apperror.Unauthenticated, "not signed in")
	}
	sess, err := s.repo.GetSession(ctx, identitydomain.HashToken(token))
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	if !sess.Active(now) {
		return nil, nil, apperror.New(apperror.Unauthenticated, "session expired")
	}
	if sess.Touch(now) {
		if err := s.repo.UpdateSession(ctx, sess); err != nil {
			return nil, nil, err
		}
	}
	u, err := s.repo.GetUser(ctx, sess.UserID())
	if apperror.Is(err, apperror.UserNotFound) {
		return nil, nil, apperror.New(apperror.Unauthenticated, "not signed in")
	}
	if err != nil {
		return nil, nil, err
	}
	return u, sess, nil
}

// Logout ends the session.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	return s.repo.DeleteSession(ctx, sessionID)
}

// Rename changes the user's display name.
func (s *Service) Rename(ctx context.Context, userID uuid.UUID, name string) (*identitydomain.User, error) {
	var out *identitydomain.User
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		u, err := s.repo.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		if err := u.Rename(name, s.now()); err != nil {
			return err
		}
		out = u
		return s.repo.UpdateUser(ctx, u)
	})
	return out, err
}

// ChangePassword replaces the password after checking the current one,
// and signs out every session but keepSession.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, keepSession, current string, next identitydomain.Password) error {
	u, err := s.repo.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.checkPassword(u, current, s.now()); err != nil {
		if apperror.Is(err, apperror.Unauthenticated) {
			// Not a sign-in failure: the caller is signed in, the old
			// password is just wrong. A 401 would sign the UI out.
			return apperror.New(apperror.ValidationFailed, "current password is wrong")
		}
		return err
	}
	hash, err := s.hasher.Hash(next.Plain())
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		u, err := s.repo.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		if err := u.SetPasswordHash(hash, s.now()); err != nil {
			return err
		}
		if err := s.repo.UpdateUser(ctx, u); err != nil {
			return err
		}
		return s.repo.DeleteUserSessions(ctx, userID, keepSession)
	})
}

// Authorize returns userID's membership in workspaceID when it allows
// action. Non-members get WorkspaceNotFound, so workspace ids do not leak.
func (s *Service) Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action identitydomain.Action) (*identitydomain.Membership, error) {
	m, err := s.repo.GetMembership(ctx, workspaceID, userID)
	if apperror.Is(err, apperror.MemberNotFound) {
		return nil, apperror.New(apperror.WorkspaceNotFound, "workspace not found")
	}
	if err != nil {
		return nil, err
	}
	if err := m.Authorize(action); err != nil {
		return nil, err
	}
	return m, nil
}

// Members lists the workspace's members.
func (s *Service) Members(ctx context.Context, workspaceID uuid.UUID) ([]Member, error) {
	return s.repo.ListMembers(ctx, workspaceID)
}

// ChangeRole sets target's role on behalf of actor.
func (s *Service) ChangeRole(ctx context.Context, workspaceID, actorID, targetID uuid.UUID, role identitydomain.Role) (Member, error) {
	var out Member
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		by, target, owners, err := s.loadPair(ctx, workspaceID, actorID, targetID)
		if err != nil {
			return err
		}
		if err := target.ChangeRole(by, role, owners, s.now()); err != nil {
			return err
		}
		if err := s.repo.UpdateMembership(ctx, target); err != nil {
			return err
		}
		u, err := s.repo.GetUser(ctx, targetID)
		if err != nil {
			return err
		}
		out = Member{Membership: target, User: u}
		return nil
	})
	return out, err
}

// RemoveMember removes target on behalf of actor (or lets actor leave).
func (s *Service) RemoveMember(ctx context.Context, workspaceID, actorID, targetID uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		by, target, owners, err := s.loadPair(ctx, workspaceID, actorID, targetID)
		if err != nil {
			return err
		}
		if err := target.CheckRemoval(by, owners); err != nil {
			return err
		}
		return s.repo.DeleteMembership(ctx, workspaceID, targetID)
	})
}

func (s *Service) loadPair(ctx context.Context, workspaceID, actorID, targetID uuid.UUID) (by, target *identitydomain.Membership, owners int, err error) {
	if by, err = s.repo.GetMembership(ctx, workspaceID, actorID); err != nil {
		if apperror.Is(err, apperror.MemberNotFound) {
			err = apperror.New(apperror.WorkspaceNotFound, "workspace not found")
		}
		return nil, nil, 0, err
	}
	if target, err = s.repo.GetMembership(ctx, workspaceID, targetID); err != nil {
		return nil, nil, 0, err
	}
	owners, err = s.repo.CountOwners(ctx, workspaceID)
	return by, target, owners, err
}

// Invite invites email to the workspace on behalf of actor and returns
// the link token. Re-inviting an address replaces its pending invitation.
func (s *Service) Invite(ctx context.Context, workspaceID, actorID uuid.UUID, email identitydomain.Email, role identitydomain.Role) (*identitydomain.Invitation, string, error) {
	var (
		inv   *identitydomain.Invitation
		token string
	)
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		by, err := s.repo.GetMembership(ctx, workspaceID, actorID)
		if err != nil {
			return err
		}
		if u, err := s.repo.GetUserByEmail(ctx, email); err == nil {
			if _, err := s.repo.GetMembership(ctx, workspaceID, u.ID()); err == nil {
				return apperror.New(apperror.AlreadyMember, email.String()+" is already a member")
			} else if !apperror.Is(err, apperror.MemberNotFound) {
				return err
			}
		} else if !apperror.Is(err, apperror.UserNotFound) {
			return err
		}
		inv, token, err = identitydomain.NewInvitation(id.New(), by, email, role, s.now())
		if err != nil {
			return err
		}
		return s.repo.CreateInvitation(ctx, inv)
	})
	if err != nil {
		return nil, "", err
	}
	return inv, token, nil
}

// Invitations lists the workspace's pending invitations.
func (s *Service) Invitations(ctx context.Context, workspaceID uuid.UUID) ([]*identitydomain.Invitation, error) {
	return s.repo.ListInvitations(ctx, workspaceID, s.now())
}

// RevokeInvitation deletes a pending invitation.
func (s *Service) RevokeInvitation(ctx context.Context, workspaceID, invitationID uuid.UUID) error {
	return s.repo.DeleteInvitation(ctx, workspaceID, invitationID)
}

// InvitationPreview is what the accept page shows.
type InvitationPreview struct {
	Invitation    *identitydomain.Invitation
	WorkspaceName string
	AccountExists bool
}

// PreviewInvitation looks up a pending invitation by its link token.
func (s *Service) PreviewInvitation(ctx context.Context, token string) (InvitationPreview, error) {
	inv, err := s.pendingInvitation(ctx, token)
	if err != nil {
		return InvitationPreview{}, err
	}
	w, err := s.workspaces.Get(ctx, inv.WorkspaceID())
	if err != nil {
		return InvitationPreview{}, err
	}
	_, err = s.repo.GetUserByEmail(ctx, inv.Email())
	if err != nil && !apperror.Is(err, apperror.UserNotFound) {
		return InvitationPreview{}, err
	}
	return InvitationPreview{Invitation: inv, WorkspaceName: w.Name(), AccountExists: err == nil}, nil
}

func (s *Service) pendingInvitation(ctx context.Context, token string) (*identitydomain.Invitation, error) {
	inv, err := s.repo.GetInvitationByToken(ctx, identitydomain.HashToken(token))
	if err != nil {
		return nil, err
	}
	if !inv.Pending(s.now()) {
		return nil, apperror.New(apperror.InvitationNotFound, "invitation not found or expired")
	}
	return inv, nil
}

// Accepted is the result of accepting an invitation.
type Accepted struct {
	SignIn
	WorkspaceID uuid.UUID
}

// AcceptInvitation joins the invited workspace and signs in. If the
// invited email has an account, password must be its password; otherwise
// name and password create the account.
func (s *Service) AcceptInvitation(ctx context.Context, token, name, password string) (Accepted, error) {
	inv, err := s.pendingInvitation(ctx, token)
	if err != nil {
		return Accepted{}, err
	}
	now := s.now()
	u, err := s.repo.GetUserByEmail(ctx, inv.Email())
	isNew := apperror.Is(err, apperror.UserNotFound)
	switch {
	case err == nil:
		if err := s.checkPassword(u, password, now); err != nil {
			return Accepted{}, err
		}
	case isNew:
		pw, err := identitydomain.ParsePassword(password)
		if err != nil {
			return Accepted{}, err
		}
		hash, err := s.hasher.Hash(pw.Plain())
		if err != nil {
			return Accepted{}, err
		}
		if u, err = identitydomain.NewUser(id.New(), inv.Email(), name, hash, now); err != nil {
			return Accepted{}, err
		}
	default:
		return Accepted{}, err
	}

	var out Accepted
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// Re-read inside the transaction: the invitation may have been
		// revoked or used since the checks above.
		inv, err := s.pendingInvitation(ctx, token)
		if err != nil {
			return err
		}
		if isNew {
			if err := s.repo.CreateUser(ctx, u); err != nil {
				return err
			}
		}
		m, err := inv.Accept(u, now)
		if err != nil {
			return err
		}
		if err := s.repo.CreateMembership(ctx, m); err != nil && !apperror.Is(err, apperror.AlreadyMember) {
			return err
		}
		if err := s.repo.DeleteInvitation(ctx, inv.WorkspaceID(), inv.ID()); err != nil {
			return err
		}
		signIn, err := s.startSession(ctx, u, now)
		if err != nil {
			return err
		}
		out = Accepted{SignIn: signIn, WorkspaceID: inv.WorkspaceID()}
		return nil
	})
	return out, err
}

// Sweep deletes expired sessions and invitations.
func (s *Service) Sweep(ctx context.Context) (sessions, invitations int, err error) {
	now := s.now()
	sessions, err1 := s.repo.DeleteExpiredSessions(ctx, now)
	invitations, err2 := s.repo.DeleteExpiredInvitations(ctx, now)
	return sessions, invitations, errors.Join(err1, err2)
}

// HasUsers reports whether any account exists.
func (s *Service) HasUsers(ctx context.Context) (bool, error) {
	n, err := s.repo.CountUsers(ctx)
	return n > 0, err
}
