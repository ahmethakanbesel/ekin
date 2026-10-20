package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db/dbtest"
	identitydomain "github.com/ahmethakanbesel/ekin/internal/domain/identity/v1"
	workspacedomain "github.com/ahmethakanbesel/ekin/internal/domain/workspace/v1"
	identityrepo "github.com/ahmethakanbesel/ekin/internal/repository/identity/v1"
	workspacerepo "github.com/ahmethakanbesel/ekin/internal/repository/workspace/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// harness is a fresh repository plus a way to create the workspace rows
// the SQLite foreign keys need.
type harness struct {
	repo      identityrepo.Repository
	workspace func(t *testing.T) uuid.UUID
}

func TestConformance(t *testing.T) {
	impls := map[string]func(t *testing.T) harness{
		"inmemory": func(*testing.T) harness {
			return harness{repo: identityrepo.NewInMemory(), workspace: func(*testing.T) uuid.UUID { return id.New() }}
		},
		"sqlite": func(t *testing.T) harness {
			_, tx := dbtest.Open(t)
			workspaces := workspacerepo.NewSQLite(tx)
			return harness{repo: identityrepo.NewSQLite(tx), workspace: func(t *testing.T) uuid.UUID {
				w, _ := workspacedomain.New(id.New(), "ws", time.UTC, now)
				if err := workspaces.Create(context.Background(), w); err != nil {
					t.Fatal(err)
				}
				return w.ID()
			}}
		},
	}
	for name, newHarness := range impls {
		t.Run(name, func(t *testing.T) { runConformance(t, newHarness) })
	}
}

var now = time.Date(2026, 10, 13, 9, 0, 0, 123_456_789, time.UTC)

func newUser(t *testing.T, repo identityrepo.Repository, email string) *identitydomain.User {
	t.Helper()
	e, err := identitydomain.ParseEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := identitydomain.NewUser(id.New(), e, "User "+email, "hash", now)
	if err := repo.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func join(t *testing.T, repo identityrepo.Repository, ws uuid.UUID, u *identitydomain.User, role identitydomain.Role, at time.Time) *identitydomain.Membership {
	t.Helper()
	m, _ := identitydomain.NewMembership(ws, u.ID(), role, at)
	if err := repo.CreateMembership(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return m
}

func runConformance(t *testing.T, newHarness func(t *testing.T) harness) {
	ctx := context.Background()

	t.Run("users", func(t *testing.T) {
		h := newHarness(t)
		u := newUser(t, h.repo, "ada@example.com")
		got, err := h.repo.GetUserByEmail(ctx, u.Email())
		if err != nil || got.Snapshot() != u.Snapshot() {
			t.Fatalf("by email = %+v, %v", got, err)
		}
		dup, _ := identitydomain.NewUser(id.New(), u.Email(), "Other", "hash", now)
		if err := h.repo.CreateUser(ctx, dup); !apperror.Is(err, apperror.EmailTaken) {
			t.Fatalf("duplicate email err = %v", err)
		}
		_ = u.Rename("Ada L.", now.Add(time.Minute))
		_ = u.SetPasswordHash("hash2", now.Add(time.Minute))
		if err := h.repo.UpdateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		got, _ = h.repo.GetUser(ctx, u.ID())
		if got.Snapshot() != u.Snapshot() {
			t.Fatalf("updated = %+v", got.Snapshot())
		}
		if _, err := h.repo.GetUser(ctx, id.New()); !apperror.Is(err, apperror.UserNotFound) {
			t.Fatalf("missing err = %v", err)
		}
		if n, _ := h.repo.CountUsers(ctx); n != 1 {
			t.Fatalf("count = %d", n)
		}
	})

	t.Run("sessions", func(t *testing.T) {
		h := newHarness(t)
		u := newUser(t, h.repo, "ada@example.com")
		a, _ := identitydomain.NewSession(u.ID(), now)
		b, _ := identitydomain.NewSession(u.ID(), now)
		old, _ := identitydomain.NewSession(u.ID(), now.Add(-identitydomain.SessionLifetime))
		for _, s := range []*identitydomain.Session{a, b, old} {
			if err := h.repo.CreateSession(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
		a.Touch(now.Add(2 * time.Hour))
		if err := h.repo.UpdateSession(ctx, a); err != nil {
			t.Fatal(err)
		}
		got, err := h.repo.GetSession(ctx, a.ID())
		if err != nil || got.Snapshot() != a.Snapshot() {
			t.Fatalf("session = %+v, %v", got, err)
		}
		if n, _ := h.repo.DeleteExpiredSessions(ctx, now); n != 1 {
			t.Fatalf("expired deleted = %d", n)
		}
		if err := h.repo.DeleteUserSessions(ctx, u.ID(), a.ID()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.repo.GetSession(ctx, b.ID()); !apperror.Is(err, apperror.Unauthenticated) {
			t.Fatalf("other session survived: %v", err)
		}
		if _, err := h.repo.GetSession(ctx, a.ID()); err != nil {
			t.Fatalf("kept session gone: %v", err)
		}
		_ = h.repo.DeleteSession(ctx, a.ID())
		if _, err := h.repo.GetSession(ctx, a.ID()); !apperror.Is(err, apperror.Unauthenticated) {
			t.Fatalf("deleted session err = %v", err)
		}
	})

	t.Run("memberships", func(t *testing.T) {
		h := newHarness(t)
		ws, other := h.workspace(t), h.workspace(t)
		viewer := newUser(t, h.repo, "viewer@example.com")
		owner := newUser(t, h.repo, "owner@example.com")
		editor := newUser(t, h.repo, "editor@example.com")
		join(t, h.repo, ws, viewer, identitydomain.RoleViewer, now)
		join(t, h.repo, ws, editor, identitydomain.RoleEditor, now.Add(time.Minute))
		ownerM := join(t, h.repo, ws, owner, identitydomain.RoleOwner, now.Add(2*time.Minute))
		join(t, h.repo, other, viewer, identitydomain.RoleOwner, now.Add(-time.Hour))

		dup, _ := identitydomain.NewMembership(ws, viewer.ID(), identitydomain.RoleEditor, now)
		if err := h.repo.CreateMembership(ctx, dup); !apperror.Is(err, apperror.AlreadyMember) {
			t.Fatalf("duplicate err = %v", err)
		}

		members, err := h.repo.ListMembers(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		for _, m := range members {
			order = append(order, string(m.User.Email()))
		}
		if len(order) != 3 || order[0] != "owner@example.com" || order[1] != "viewer@example.com" || order[2] != "editor@example.com" {
			t.Fatalf("members order = %v", order)
		}
		if members[0].Membership.Snapshot() != ownerM.Snapshot() {
			t.Fatalf("member = %+v", members[0].Membership.Snapshot())
		}

		mine, _ := h.repo.ListUserMemberships(ctx, viewer.ID())
		if len(mine) != 2 || mine[0].WorkspaceID() != other {
			t.Fatalf("user memberships = %d", len(mine))
		}

		if n, _ := h.repo.CountOwners(ctx, ws); n != 1 {
			t.Fatalf("owners = %d", n)
		}
		m, _ := h.repo.GetMembership(ctx, ws, editor.ID())
		_ = m.ChangeRole(ownerM, identitydomain.RoleOwner, 1, now.Add(time.Hour))
		if err := h.repo.UpdateMembership(ctx, m); err != nil {
			t.Fatal(err)
		}
		if n, _ := h.repo.CountOwners(ctx, ws); n != 2 {
			t.Fatalf("owners after promote = %d", n)
		}
		if err := h.repo.DeleteMembership(ctx, ws, viewer.ID()); err != nil {
			t.Fatal(err)
		}
		if n, _ := h.repo.CountMembers(ctx, ws); n != 2 {
			t.Fatalf("members after delete = %d", n)
		}
		if _, err := h.repo.GetMembership(ctx, ws, viewer.ID()); !apperror.Is(err, apperror.MemberNotFound) {
			t.Fatalf("deleted membership err = %v", err)
		}
		if err := h.repo.DeleteMembership(ctx, ws, viewer.ID()); !apperror.Is(err, apperror.MemberNotFound) {
			t.Fatalf("double delete err = %v", err)
		}
	})

	t.Run("invitations", func(t *testing.T) {
		h := newHarness(t)
		ws := h.workspace(t)
		owner := newUser(t, h.repo, "owner@example.com")
		ownerM := join(t, h.repo, ws, owner, identitydomain.RoleOwner, now)
		email, _ := identitydomain.ParseEmail("new@example.com")

		first, firstToken, _ := identitydomain.NewInvitation(id.New(), ownerM, email, identitydomain.RoleViewer, now)
		if err := h.repo.CreateInvitation(ctx, first); err != nil {
			t.Fatal(err)
		}
		second, secondToken, _ := identitydomain.NewInvitation(id.New(), ownerM, email, identitydomain.RoleEditor, now.Add(time.Minute))
		if err := h.repo.CreateInvitation(ctx, second); err != nil {
			t.Fatal(err)
		}
		if _, err := h.repo.GetInvitationByToken(ctx, identitydomain.HashToken(firstToken)); !apperror.Is(err, apperror.InvitationNotFound) {
			t.Fatalf("replaced invitation still works: %v", err)
		}
		got, err := h.repo.GetInvitationByToken(ctx, identitydomain.HashToken(secondToken))
		if err != nil || got.Snapshot() != second.Snapshot() {
			t.Fatalf("invitation = %+v, %v", got, err)
		}

		otherEmail, _ := identitydomain.ParseEmail("later@example.com")
		later, _, _ := identitydomain.NewInvitation(id.New(), ownerM, otherEmail, identitydomain.RoleViewer, now.Add(2*time.Minute))
		_ = h.repo.CreateInvitation(ctx, later)
		pending, _ := h.repo.ListInvitations(ctx, ws, now.Add(time.Hour))
		if len(pending) != 2 || pending[0].ID() != later.ID() {
			t.Fatalf("pending = %d", len(pending))
		}
		expiredAt := now.Add(time.Minute + identitydomain.InvitationTTL)
		if pending, _ := h.repo.ListInvitations(ctx, ws, expiredAt); len(pending) != 1 {
			t.Fatalf("pending after expiry = %d", len(pending))
		}
		if n, _ := h.repo.DeleteExpiredInvitations(ctx, expiredAt); n != 1 {
			t.Fatalf("expired deleted = %d", n)
		}
		if err := h.repo.DeleteInvitation(ctx, ws, later.ID()); err != nil {
			t.Fatal(err)
		}
		if err := h.repo.DeleteInvitation(ctx, ws, later.ID()); !apperror.Is(err, apperror.InvitationNotFound) {
			t.Fatalf("double delete err = %v", err)
		}
	})
}
