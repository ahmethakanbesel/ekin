package identity_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	identity "kurgu/internal/domain/identity/v1"
	"kurgu/pkg/id"
)

var now = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

func TestParseEmail(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"ada@example.com", "ada@example.com", true},
		{"  Ada@Example.COM ", "ada@example.com", true},
		{"", "", false},
		{"ada", "", false},
		{"Ada <ada@example.com>", "", false},
		{"ada@example.com, bob@example.com", "", false},
		{strings.Repeat("a", 250) + "@x.io", "", false},
	}
	for _, tt := range tests {
		got, err := identity.ParseEmail(tt.in)
		if (err == nil) != tt.ok || string(got) != tt.want {
			t.Errorf("ParseEmail(%q) = %q, %v", tt.in, got, err)
		}
	}
}

func TestParsePassword(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"short", false},
		{"1234567", false},
		{"12345678", true},
		{"0123456789", true},
		{"çğıöşüçğ", true}, // runes, not bytes
		{"çğıöşüç", false},
		{"        ", false},
		{strings.Repeat("x", 128), true},
		{strings.Repeat("x", 129), false},
	}
	for _, tt := range tests {
		p, err := identity.ParsePassword(tt.in)
		if (err == nil) != tt.ok {
			t.Errorf("ParsePassword(%q) err = %v", tt.in, err)
		}
		if err == nil && (p.Plain() != tt.in || fmt.Sprint(p) != "[redacted]" || fmt.Sprintf("%#v", p) != "[redacted]") {
			t.Errorf("password leaks or loses its value: %v", p)
		}
	}
}

func TestRoleCan(t *testing.T) {
	tests := []struct {
		role               identity.Role
		view, edit, manage bool
	}{
		{identity.RoleOwner, true, true, true},
		{identity.RoleEditor, true, true, false},
		{identity.RoleViewer, true, false, false},
		{identity.Role("admin"), false, false, false},
	}
	for _, tt := range tests {
		got := []bool{tt.role.Can(identity.ActionView), tt.role.Can(identity.ActionEdit), tt.role.Can(identity.ActionManage)}
		if got[0] != tt.view || got[1] != tt.edit || got[2] != tt.manage {
			t.Errorf("%s can = %v", tt.role, got)
		}
	}
}

func TestSessionExpiry(t *testing.T) {
	s, token := identity.NewSession(id.New(), now)
	if len(token) != 43 || s.ID() != identity.HashToken(token) || s.ID() == token {
		t.Fatalf("token %q hash %q", token, s.ID())
	}
	if !s.ExpiresAt().Equal(now.Add(identity.SessionIdle)) {
		t.Fatalf("expires = %v", s.ExpiresAt())
	}
	if s.Touch(now.Add(30 * time.Minute)) {
		t.Fatal("touch within the hour must not write")
	}
	// Used every 20 days: the idle window slides, the lifetime caps it.
	at := now
	for range 4 {
		at = at.Add(20 * 24 * time.Hour)
		if !s.Active(at) || !s.Touch(at) {
			t.Fatalf("session dead at %v", at)
		}
	}
	if want := now.Add(identity.SessionLifetime); !s.ExpiresAt().Equal(want) {
		t.Fatalf("expires = %v, want lifetime cap %v", s.ExpiresAt(), want)
	}
	if s.Active(now.Add(identity.SessionLifetime)) {
		t.Fatal("session outlived its lifetime")
	}
	idle, _ := identity.NewSession(id.New(), now)
	if idle.Active(now.Add(identity.SessionIdle)) || idle.Touch(now.Add(identity.SessionIdle)) {
		t.Fatal("idle session revived")
	}
}

type members struct {
	ws                    uuid.UUID
	owner, owner2, editor *identity.Membership
	viewer                *identity.Membership
}

func newMembers(t *testing.T) members {
	t.Helper()
	ws := id.New()
	mk := func(r identity.Role) *identity.Membership {
		m, err := identity.NewMembership(ws, id.New(), r, now)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	return members{ws: ws, owner: mk(identity.RoleOwner), owner2: mk(identity.RoleOwner), editor: mk(identity.RoleEditor), viewer: mk(identity.RoleViewer)}
}

func TestChangeRole(t *testing.T) {
	tests := []struct {
		name     string
		by       func(m members) *identity.Membership
		target   func(m members) *identity.Membership
		role     identity.Role
		owners   int
		wantCode apperror.Code
	}{
		{"owner promotes editor", func(m members) *identity.Membership { return m.owner }, func(m members) *identity.Membership { return m.editor }, identity.RoleOwner, 1, ""},
		{"editor cannot change roles", func(m members) *identity.Membership { return m.editor }, func(m members) *identity.Membership { return m.viewer }, identity.RoleEditor, 1, apperror.Forbidden},
		{"last owner cannot step down", func(m members) *identity.Membership { return m.owner }, func(m members) *identity.Membership { return m.owner }, identity.RoleEditor, 1, apperror.LastOwner},
		{"owner steps down when another remains", func(m members) *identity.Membership { return m.owner }, func(m members) *identity.Membership { return m.owner }, identity.RoleEditor, 2, ""},
		{"unknown role", func(m members) *identity.Membership { return m.owner }, func(m members) *identity.Membership { return m.viewer }, identity.Role("admin"), 1, apperror.ValidationFailed},
		{"other workspace", func(members) *identity.Membership {
			o, _ := identity.NewMembership(id.New(), id.New(), identity.RoleOwner, now)
			return o
		}, func(m members) *identity.Membership { return m.viewer }, identity.RoleEditor, 1, apperror.Forbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMembers(t)
			target := tt.target(m)
			before := target.Role()
			err := target.ChangeRole(tt.by(m), tt.role, tt.owners, now.Add(time.Minute))
			if tt.wantCode == "" {
				if err != nil || target.Role() != tt.role {
					t.Fatalf("err = %v role = %s", err, target.Role())
				}
				return
			}
			if !apperror.Is(err, tt.wantCode) || target.Role() != before {
				t.Fatalf("err = %v (want %s), role %s", err, tt.wantCode, target.Role())
			}
		})
	}
}

func TestCheckRemoval(t *testing.T) {
	tests := []struct {
		name     string
		by       func(m members) *identity.Membership
		target   func(m members) *identity.Membership
		owners   int
		wantCode apperror.Code
	}{
		{"owner removes viewer", func(m members) *identity.Membership { return m.owner }, func(m members) *identity.Membership { return m.viewer }, 1, ""},
		{"viewer leaves", func(m members) *identity.Membership { return m.viewer }, func(m members) *identity.Membership { return m.viewer }, 1, ""},
		{"editor cannot remove others", func(m members) *identity.Membership { return m.editor }, func(m members) *identity.Membership { return m.viewer }, 1, apperror.Forbidden},
		{"last owner cannot leave", func(m members) *identity.Membership { return m.owner }, func(m members) *identity.Membership { return m.owner }, 1, apperror.LastOwner},
		{"owner removes co-owner", func(m members) *identity.Membership { return m.owner }, func(m members) *identity.Membership { return m.owner2 }, 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMembers(t)
			err := tt.target(m).CheckRemoval(tt.by(m), tt.owners)
			if (tt.wantCode == "" && err != nil) || (tt.wantCode != "" && !apperror.Is(err, tt.wantCode)) {
				t.Fatalf("err = %v, want %q", err, tt.wantCode)
			}
		})
	}
}

func TestInvitation(t *testing.T) {
	m := newMembers(t)
	email, _ := identity.ParseEmail("new@example.com")
	if _, _, err := identity.NewInvitation(id.New(), m.editor, email, identity.RoleViewer, now); !apperror.Is(err, apperror.Forbidden) {
		t.Fatalf("editor invited: %v", err)
	}
	inv, token, err := identity.NewInvitation(id.New(), m.owner, email, identity.RoleEditor, now)
	if err != nil {
		t.Fatal(err)
	}
	if inv.TokenHash() != identity.HashToken(token) || inv.WorkspaceID() != m.ws || inv.InvitedBy() != m.owner.UserID() {
		t.Fatalf("invitation = %+v", inv.Snapshot())
	}

	other, _ := identity.ParseEmail("other@example.com")
	stranger, _ := identity.NewUser(id.New(), other, "Stranger", "hash", now)
	if _, err := inv.Accept(stranger, now); !apperror.Is(err, apperror.Forbidden) {
		t.Fatalf("wrong email accepted: %v", err)
	}
	invitee, _ := identity.NewUser(id.New(), email, "Invitee", "hash", now)
	if _, err := inv.Accept(invitee, now.Add(identity.InvitationTTL)); !apperror.Is(err, apperror.InvitationNotFound) {
		t.Fatalf("expired accepted: %v", err)
	}
	joined, err := inv.Accept(invitee, now.Add(time.Hour))
	if err != nil || joined.UserID() != invitee.ID() || joined.WorkspaceID() != m.ws || joined.Role() != identity.RoleEditor {
		t.Fatalf("accept = %+v, %v", joined, err)
	}
}

func TestSnapshotsRoundTrip(t *testing.T) {
	email, _ := identity.ParseEmail("ada@example.com")
	u, _ := identity.NewUser(id.New(), email, "Ada", "hash", now)
	if got, err := identity.RehydrateUser(u.Snapshot()); err != nil || got.Snapshot() != u.Snapshot() {
		t.Fatalf("user = %+v, %v", got, err)
	}
	s, _ := identity.NewSession(u.ID(), now)
	if got, err := identity.RehydrateSession(s.Snapshot()); err != nil || got.Snapshot() != s.Snapshot() {
		t.Fatalf("session = %+v, %v", got, err)
	}
	m, _ := identity.NewMembership(id.New(), u.ID(), identity.RoleEditor, now)
	if got, err := identity.RehydrateMembership(m.Snapshot()); err != nil || got.Snapshot() != m.Snapshot() {
		t.Fatalf("membership = %+v, %v", got, err)
	}
	owner, _ := identity.NewMembership(m.WorkspaceID(), id.New(), identity.RoleOwner, now)
	inv, _, _ := identity.NewInvitation(id.New(), owner, email, identity.RoleViewer, now)
	if got, err := identity.RehydrateInvitation(inv.Snapshot()); err != nil || got.Snapshot() != inv.Snapshot() {
		t.Fatalf("invitation = %+v, %v", got, err)
	}
}
