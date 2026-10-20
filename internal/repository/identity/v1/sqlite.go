package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	identitydomain "github.com/ahmethakanbesel/ekin/internal/domain/identity/v1"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// SQLite is the production Repository.
type SQLite struct {
	q db.QuerierSource
}

var _ Repository = (*SQLite)(nil)

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite {
	return &SQLite{q: q}
}

type scanner interface {
	Scan(dest ...any) error
}

// --- users

const userColumns = `id, email, name, password_hash, is_admin, email_notifications, created_at, updated_at`

func (r *SQLite) CreateUser(ctx context.Context, u *identitydomain.User) error {
	s := u.Snapshot()
	_, err := r.q.W(ctx).ExecContext(ctx,
		`INSERT INTO users (`+userColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID.String(), s.Email, s.Name, s.PasswordHash, s.Admin, s.EmailNotify, timeutil.ToMillis(s.CreatedAt), timeutil.ToMillis(s.UpdatedAt))
	if db.IsUniqueViolation(err) {
		return apperror.New(apperror.EmailTaken, "an account with this email already exists")
	}
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *SQLite) UpdateUser(ctx context.Context, u *identitydomain.User) error {
	s := u.Snapshot()
	res, err := r.q.W(ctx).ExecContext(ctx,
		`UPDATE users SET email = ?, name = ?, password_hash = ?, is_admin = ?, email_notifications = ?, updated_at = ? WHERE id = ?`,
		s.Email, s.Name, s.PasswordHash, s.Admin, s.EmailNotify, timeutil.ToMillis(s.UpdatedAt), s.ID.String())
	if db.IsUniqueViolation(err) {
		return apperror.New(apperror.EmailTaken, "an account with this email already exists")
	}
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.UserNotFound, "user not found")
	}
	return nil
}

func (r *SQLite) GetUser(ctx context.Context, id uuid.UUID) (*identitydomain.User, error) {
	return r.getUser(ctx, `id = ?`, id.String())
}

func (r *SQLite) GetUserByEmail(ctx context.Context, email identitydomain.Email) (*identitydomain.User, error) {
	return r.getUser(ctx, `email = ?`, string(email))
}

func (r *SQLite) getUser(ctx context.Context, where string, arg any) (*identitydomain.User, error) {
	row := r.q.R(ctx).QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE `+where, arg)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.New(apperror.UserNotFound, "user not found")
	}
	return u, err
}

func (r *SQLite) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := r.q.R(ctx).QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func scanUser(s scanner) (*identitydomain.User, error) {
	var (
		rawID              string
		snap               identitydomain.UserSnapshot
		createdAt, updated int64
	)
	if err := s.Scan(&rawID, &snap.Email, &snap.Name, &snap.PasswordHash, &snap.Admin, &snap.EmailNotify, &createdAt, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}
	snap.ID = id
	snap.CreatedAt = timeutil.FromMillis(createdAt)
	snap.UpdatedAt = timeutil.FromMillis(updated)
	return identitydomain.RehydrateUser(snap)
}

// --- sessions

const sessionColumns = `id, user_id, created_at, last_seen_at, expires_at`

func (r *SQLite) CreateSession(ctx context.Context, s *identitydomain.Session) error {
	snap := s.Snapshot()
	_, err := r.q.W(ctx).ExecContext(ctx,
		`INSERT INTO sessions (`+sessionColumns+`) VALUES (?, ?, ?, ?, ?)`,
		snap.ID, snap.UserID.String(), timeutil.ToMillis(snap.CreatedAt), timeutil.ToMillis(snap.LastSeenAt), timeutil.ToMillis(snap.ExpiresAt))
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *SQLite) GetSession(ctx context.Context, id string) (*identitydomain.Session, error) {
	var (
		snap                         identitydomain.SessionSnapshot
		rawUser                      string
		createdAt, lastSeen, expires int64
	)
	err := r.q.R(ctx).QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id).
		Scan(&snap.ID, &rawUser, &createdAt, &lastSeen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.New(apperror.Unauthenticated, "not signed in")
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if snap.UserID, err = uuid.Parse(rawUser); err != nil {
		return nil, fmt.Errorf("parse session user: %w", err)
	}
	snap.CreatedAt = timeutil.FromMillis(createdAt)
	snap.LastSeenAt = timeutil.FromMillis(lastSeen)
	snap.ExpiresAt = timeutil.FromMillis(expires)
	return identitydomain.RehydrateSession(snap)
}

func (r *SQLite) UpdateSession(ctx context.Context, s *identitydomain.Session) error {
	snap := s.Snapshot()
	_, err := r.q.W(ctx).ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`,
		timeutil.ToMillis(snap.LastSeenAt), timeutil.ToMillis(snap.ExpiresAt), snap.ID)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

func (r *SQLite) DeleteSession(ctx context.Context, id string) error {
	if _, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *SQLite) DeleteUserSessions(ctx context.Context, userID uuid.UUID, keep string) error {
	if _, err := r.q.W(ctx).ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID.String(), keep); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

func (r *SQLite) DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error) {
	res, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, timeutil.ToMillis(now))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// --- memberships

const membershipColumns = `m.workspace_id, m.user_id, m.role, m.created_at, m.updated_at`

func (r *SQLite) CreateMembership(ctx context.Context, m *identitydomain.Membership) error {
	s := m.Snapshot()
	_, err := r.q.W(ctx).ExecContext(ctx,
		`INSERT INTO memberships (workspace_id, user_id, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		s.WorkspaceID.String(), s.UserID.String(), s.Role, timeutil.ToMillis(s.CreatedAt), timeutil.ToMillis(s.UpdatedAt))
	if db.IsUniqueViolation(err) {
		return apperror.New(apperror.AlreadyMember, "already a member of this workspace")
	}
	if err != nil {
		return fmt.Errorf("insert membership: %w", err)
	}
	return nil
}

func (r *SQLite) UpdateMembership(ctx context.Context, m *identitydomain.Membership) error {
	s := m.Snapshot()
	res, err := r.q.W(ctx).ExecContext(ctx,
		`UPDATE memberships SET role = ?, updated_at = ? WHERE workspace_id = ? AND user_id = ?`,
		s.Role, timeutil.ToMillis(s.UpdatedAt), s.WorkspaceID.String(), s.UserID.String())
	if err != nil {
		return fmt.Errorf("update membership: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.MemberNotFound, "member not found")
	}
	return nil
}

func (r *SQLite) GetMembership(ctx context.Context, workspaceID, userID uuid.UUID) (*identitydomain.Membership, error) {
	row := r.q.R(ctx).QueryRowContext(ctx,
		`SELECT `+membershipColumns+` FROM memberships m WHERE m.workspace_id = ? AND m.user_id = ?`,
		workspaceID.String(), userID.String())
	m, err := scanMembership(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.New(apperror.MemberNotFound, "member not found")
	}
	return m, err
}

func (r *SQLite) DeleteMembership(ctx context.Context, workspaceID, userID uuid.UUID) error {
	res, err := r.q.W(ctx).ExecContext(ctx,
		`DELETE FROM memberships WHERE workspace_id = ? AND user_id = ?`, workspaceID.String(), userID.String())
	if err != nil {
		return fmt.Errorf("delete membership: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.MemberNotFound, "member not found")
	}
	return nil
}

func (r *SQLite) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]Member, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx,
		`SELECT `+membershipColumns+`, u.id, u.email, u.name, u.password_hash, u.is_admin, u.email_notifications, u.created_at, u.updated_at
		   FROM memberships m JOIN users u ON u.id = m.user_id
		  WHERE m.workspace_id = ?
		  ORDER BY m.role <> 'owner', m.created_at, u.email`, workspaceID.String())
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Member
	for rows.Next() {
		var (
			ms                   identitydomain.MembershipSnapshot
			us                   identitydomain.UserSnapshot
			rawWS, rawUser, rawU string
			mc, mu, uc, uu       int64
		)
		if err := rows.Scan(&rawWS, &rawUser, &ms.Role, &mc, &mu, &rawU, &us.Email, &us.Name, &us.PasswordHash, &us.Admin, &us.EmailNotify, &uc, &uu); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		var errs [3]error
		ms.WorkspaceID, errs[0] = uuid.Parse(rawWS)
		ms.UserID, errs[1] = uuid.Parse(rawUser)
		us.ID, errs[2] = uuid.Parse(rawU)
		if err := errors.Join(errs[:]...); err != nil {
			return nil, fmt.Errorf("parse member ids: %w", err)
		}
		ms.CreatedAt, ms.UpdatedAt = timeutil.FromMillis(mc), timeutil.FromMillis(mu)
		us.CreatedAt, us.UpdatedAt = timeutil.FromMillis(uc), timeutil.FromMillis(uu)
		m, err := identitydomain.RehydrateMembership(ms)
		if err != nil {
			return nil, err
		}
		u, err := identitydomain.RehydrateUser(us)
		if err != nil {
			return nil, err
		}
		out = append(out, Member{Membership: m, User: u})
	}
	return out, rows.Err()
}

func (r *SQLite) ListUserMemberships(ctx context.Context, userID uuid.UUID) ([]*identitydomain.Membership, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx,
		`SELECT `+membershipColumns+` FROM memberships m WHERE m.user_id = ? ORDER BY m.created_at, m.workspace_id`,
		userID.String())
	if err != nil {
		return nil, fmt.Errorf("list user memberships: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*identitydomain.Membership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *SQLite) CountOwners(ctx context.Context, workspaceID uuid.UUID) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM memberships WHERE workspace_id = ? AND role = 'owner'`, workspaceID.String())
}

func (r *SQLite) CountMembers(ctx context.Context, workspaceID uuid.UUID) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM memberships WHERE workspace_id = ?`, workspaceID.String())
}

func (r *SQLite) count(ctx context.Context, query string, args ...any) (int, error) {
	var n int
	if err := r.q.R(ctx).QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	return n, nil
}

func scanMembership(s scanner) (*identitydomain.Membership, error) {
	var (
		snap           identitydomain.MembershipSnapshot
		rawWS, rawUser string
		created, upd   int64
	)
	if err := s.Scan(&rawWS, &rawUser, &snap.Role, &created, &upd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan membership: %w", err)
	}
	var err error
	if snap.WorkspaceID, err = uuid.Parse(rawWS); err != nil {
		return nil, fmt.Errorf("parse membership workspace: %w", err)
	}
	if snap.UserID, err = uuid.Parse(rawUser); err != nil {
		return nil, fmt.Errorf("parse membership user: %w", err)
	}
	snap.CreatedAt, snap.UpdatedAt = timeutil.FromMillis(created), timeutil.FromMillis(upd)
	return identitydomain.RehydrateMembership(snap)
}

// --- invitations

const invitationColumns = `id, workspace_id, email, role, invited_by, token_hash, created_at, expires_at`

func (r *SQLite) CreateInvitation(ctx context.Context, i *identitydomain.Invitation) error {
	s := i.Snapshot()
	var invitedBy sql.NullString
	if s.InvitedBy != uuid.Nil {
		invitedBy = sql.NullString{String: s.InvitedBy.String(), Valid: true}
	}
	_, err := r.q.W(ctx).ExecContext(ctx,
		`INSERT INTO invitations (`+invitationColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (workspace_id, email) DO UPDATE SET
		   id = excluded.id, role = excluded.role, invited_by = excluded.invited_by,
		   token_hash = excluded.token_hash, created_at = excluded.created_at, expires_at = excluded.expires_at`,
		s.ID.String(), s.WorkspaceID.String(), s.Email, s.Role, invitedBy, s.TokenHash,
		timeutil.ToMillis(s.CreatedAt), timeutil.ToMillis(s.ExpiresAt))
	if err != nil {
		return fmt.Errorf("insert invitation: %w", err)
	}
	return nil
}

func (r *SQLite) GetInvitationByToken(ctx context.Context, tokenHash string) (*identitydomain.Invitation, error) {
	row := r.q.R(ctx).QueryRowContext(ctx,
		`SELECT `+invitationColumns+` FROM invitations WHERE token_hash = ?`, tokenHash)
	i, err := scanInvitation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.New(apperror.InvitationNotFound, "invitation not found or expired")
	}
	return i, err
}

func (r *SQLite) ListInvitations(ctx context.Context, workspaceID uuid.UUID, now time.Time) ([]*identitydomain.Invitation, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx,
		`SELECT `+invitationColumns+` FROM invitations
		  WHERE workspace_id = ? AND expires_at > ?
		  ORDER BY created_at DESC, id DESC`, workspaceID.String(), timeutil.ToMillis(now))
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*identitydomain.Invitation
	for rows.Next() {
		i, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *SQLite) DeleteInvitation(ctx context.Context, workspaceID, id uuid.UUID) error {
	res, err := r.q.W(ctx).ExecContext(ctx,
		`DELETE FROM invitations WHERE workspace_id = ? AND id = ?`, workspaceID.String(), id.String())
	if err != nil {
		return fmt.Errorf("delete invitation: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.New(apperror.InvitationNotFound, "invitation not found")
	}
	return nil
}

func (r *SQLite) DeleteExpiredInvitations(ctx context.Context, now time.Time) (int, error) {
	res, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM invitations WHERE expires_at <= ?`, timeutil.ToMillis(now))
	if err != nil {
		return 0, fmt.Errorf("delete expired invitations: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func scanInvitation(s scanner) (*identitydomain.Invitation, error) {
	var (
		snap             identitydomain.InvitationSnapshot
		rawID, rawWS     string
		invitedBy        sql.NullString
		created, expires int64
	)
	if err := s.Scan(&rawID, &rawWS, &snap.Email, &snap.Role, &invitedBy, &snap.TokenHash, &created, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan invitation: %w", err)
	}
	var err error
	if snap.ID, err = uuid.Parse(rawID); err != nil {
		return nil, fmt.Errorf("parse invitation id: %w", err)
	}
	if snap.WorkspaceID, err = uuid.Parse(rawWS); err != nil {
		return nil, fmt.Errorf("parse invitation workspace: %w", err)
	}
	if invitedBy.Valid {
		if snap.InvitedBy, err = uuid.Parse(invitedBy.String); err != nil {
			return nil, fmt.Errorf("parse invitation inviter: %w", err)
		}
	}
	snap.CreatedAt, snap.ExpiresAt = timeutil.FromMillis(created), timeutil.FromMillis(expires)
	return identitydomain.RehydrateInvitation(snap)
}
