package identity

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	identitydomain "ekin/internal/domain/identity/v1"
)

type membershipKey struct{ workspaceID, userID uuid.UUID }

type inviteKey struct {
	workspaceID uuid.UUID
	email       string
}

// InMemory is a Repository for service tests. It stores snapshots so
// callers cannot reach into the store through an aggregate. Deleting a
// user does not cascade; nothing deletes users yet.
type InMemory struct {
	mu          sync.RWMutex
	users       map[uuid.UUID]identitydomain.UserSnapshot
	sessions    map[string]identitydomain.SessionSnapshot
	memberships map[membershipKey]identitydomain.MembershipSnapshot
	invitations map[inviteKey]identitydomain.InvitationSnapshot
}

var _ Repository = (*InMemory)(nil)

// NewInMemory returns an empty InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{
		users:       map[uuid.UUID]identitydomain.UserSnapshot{},
		sessions:    map[string]identitydomain.SessionSnapshot{},
		memberships: map[membershipKey]identitydomain.MembershipSnapshot{},
		invitations: map[inviteKey]identitydomain.InvitationSnapshot{},
	}
}

// --- users

func (r *InMemory) CreateUser(_ context.Context, u *identitydomain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := u.Snapshot()
	if _, ok := r.users[s.ID]; ok {
		return apperror.New(apperror.Conflict, "user already exists")
	}
	if r.userByEmail(s.Email) != nil {
		return apperror.New(apperror.EmailTaken, "an account with this email already exists")
	}
	r.users[s.ID] = s
	return nil
}

func (r *InMemory) UpdateUser(_ context.Context, u *identitydomain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := u.Snapshot()
	if _, ok := r.users[s.ID]; !ok {
		return apperror.New(apperror.UserNotFound, "user not found")
	}
	if other := r.userByEmail(s.Email); other != nil && other.ID != s.ID {
		return apperror.New(apperror.EmailTaken, "an account with this email already exists")
	}
	r.users[s.ID] = s
	return nil
}

func (r *InMemory) GetUser(_ context.Context, id uuid.UUID) (*identitydomain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.users[id]
	if !ok {
		return nil, apperror.New(apperror.UserNotFound, "user not found")
	}
	return identitydomain.RehydrateUser(s)
}

func (r *InMemory) GetUserByEmail(_ context.Context, email identitydomain.Email) (*identitydomain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.userByEmail(string(email))
	if s == nil {
		return nil, apperror.New(apperror.UserNotFound, "user not found")
	}
	return identitydomain.RehydrateUser(*s)
}

func (r *InMemory) CountUsers(context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.users), nil
}

func (r *InMemory) userByEmail(email string) *identitydomain.UserSnapshot {
	for _, s := range r.users {
		if s.Email == email {
			return &s
		}
	}
	return nil
}

// --- sessions

func (r *InMemory) CreateSession(_ context.Context, s *identitydomain.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := s.Snapshot()
	if _, ok := r.users[snap.UserID]; !ok {
		return apperror.New(apperror.UserNotFound, "user not found")
	}
	r.sessions[snap.ID] = snap
	return nil
}

func (r *InMemory) GetSession(_ context.Context, id string) (*identitydomain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, apperror.New(apperror.Unauthenticated, "not signed in")
	}
	return identitydomain.RehydrateSession(s)
}

func (r *InMemory) UpdateSession(_ context.Context, s *identitydomain.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := s.Snapshot()
	if _, ok := r.sessions[snap.ID]; ok {
		r.sessions[snap.ID] = snap
	}
	return nil
}

func (r *InMemory) DeleteSession(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
	return nil
}

func (r *InMemory) DeleteUserSessions(_ context.Context, userID uuid.UUID, keep string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, s := range r.sessions {
		if s.UserID == userID && id != keep {
			delete(r.sessions, id)
		}
	}
	return nil
}

func (r *InMemory) DeleteExpiredSessions(_ context.Context, now time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for id, s := range r.sessions {
		if !now.Before(s.ExpiresAt) {
			delete(r.sessions, id)
			n++
		}
	}
	return n, nil
}

// --- memberships

func (r *InMemory) CreateMembership(_ context.Context, m *identitydomain.Membership) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := m.Snapshot()
	k := membershipKey{s.WorkspaceID, s.UserID}
	if _, ok := r.memberships[k]; ok {
		return apperror.New(apperror.AlreadyMember, "already a member of this workspace")
	}
	r.memberships[k] = s
	return nil
}

func (r *InMemory) UpdateMembership(_ context.Context, m *identitydomain.Membership) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := m.Snapshot()
	k := membershipKey{s.WorkspaceID, s.UserID}
	if _, ok := r.memberships[k]; !ok {
		return apperror.New(apperror.MemberNotFound, "member not found")
	}
	r.memberships[k] = s
	return nil
}

func (r *InMemory) GetMembership(_ context.Context, workspaceID, userID uuid.UUID) (*identitydomain.Membership, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.memberships[membershipKey{workspaceID, userID}]
	if !ok {
		return nil, apperror.New(apperror.MemberNotFound, "member not found")
	}
	return identitydomain.RehydrateMembership(s)
}

func (r *InMemory) DeleteMembership(_ context.Context, workspaceID, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := membershipKey{workspaceID, userID}
	if _, ok := r.memberships[k]; !ok {
		return apperror.New(apperror.MemberNotFound, "member not found")
	}
	delete(r.memberships, k)
	return nil
}

func (r *InMemory) ListMembers(_ context.Context, workspaceID uuid.UUID) ([]Member, error) {
	r.mu.RLock()
	type pair struct {
		m identitydomain.MembershipSnapshot
		u identitydomain.UserSnapshot
	}
	var pairs []pair
	for k, m := range r.memberships {
		if k.workspaceID != workspaceID {
			continue
		}
		if u, ok := r.users[k.userID]; ok {
			pairs = append(pairs, pair{m, u})
		}
	}
	r.mu.RUnlock()
	slices.SortFunc(pairs, func(a, b pair) int {
		aOwner, bOwner := a.m.Role == string(identitydomain.RoleOwner), b.m.Role == string(identitydomain.RoleOwner)
		if aOwner != bOwner {
			if aOwner {
				return -1
			}
			return 1
		}
		return cmp.Or(a.m.CreatedAt.Compare(b.m.CreatedAt), cmp.Compare(a.u.Email, b.u.Email))
	})
	out := make([]Member, 0, len(pairs))
	for _, p := range pairs {
		m, err := identitydomain.RehydrateMembership(p.m)
		if err != nil {
			return nil, err
		}
		u, err := identitydomain.RehydrateUser(p.u)
		if err != nil {
			return nil, err
		}
		out = append(out, Member{Membership: m, User: u})
	}
	return out, nil
}

func (r *InMemory) ListUserMemberships(_ context.Context, userID uuid.UUID) ([]*identitydomain.Membership, error) {
	r.mu.RLock()
	var snaps []identitydomain.MembershipSnapshot
	for k, m := range r.memberships {
		if k.userID == userID {
			snaps = append(snaps, m)
		}
	}
	r.mu.RUnlock()
	slices.SortFunc(snaps, func(a, b identitydomain.MembershipSnapshot) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.WorkspaceID.String(), b.WorkspaceID.String()))
	})
	out := make([]*identitydomain.Membership, 0, len(snaps))
	for _, s := range snaps {
		m, err := identitydomain.RehydrateMembership(s)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *InMemory) CountOwners(_ context.Context, workspaceID uuid.UUID) (int, error) {
	return r.countMembers(workspaceID, func(s identitydomain.MembershipSnapshot) bool {
		return s.Role == string(identitydomain.RoleOwner)
	}), nil
}

func (r *InMemory) CountMembers(_ context.Context, workspaceID uuid.UUID) (int, error) {
	return r.countMembers(workspaceID, func(identitydomain.MembershipSnapshot) bool { return true }), nil
}

func (r *InMemory) countMembers(workspaceID uuid.UUID, match func(identitydomain.MembershipSnapshot) bool) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for k, m := range r.memberships {
		if k.workspaceID == workspaceID && match(m) {
			n++
		}
	}
	return n
}

// --- invitations

func (r *InMemory) CreateInvitation(_ context.Context, i *identitydomain.Invitation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := i.Snapshot()
	r.invitations[inviteKey{s.WorkspaceID, s.Email}] = s
	return nil
}

func (r *InMemory) GetInvitationByToken(_ context.Context, tokenHash string) (*identitydomain.Invitation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.invitations {
		if s.TokenHash == tokenHash {
			return identitydomain.RehydrateInvitation(s)
		}
	}
	return nil, apperror.New(apperror.InvitationNotFound, "invitation not found or expired")
}

func (r *InMemory) ListInvitations(_ context.Context, workspaceID uuid.UUID, now time.Time) ([]*identitydomain.Invitation, error) {
	r.mu.RLock()
	var snaps []identitydomain.InvitationSnapshot
	for k, s := range r.invitations {
		if k.workspaceID == workspaceID && now.Before(s.ExpiresAt) {
			snaps = append(snaps, s)
		}
	}
	r.mu.RUnlock()
	slices.SortFunc(snaps, func(a, b identitydomain.InvitationSnapshot) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID.String(), a.ID.String()))
	})
	out := make([]*identitydomain.Invitation, 0, len(snaps))
	for _, s := range snaps {
		i, err := identitydomain.RehydrateInvitation(s)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

func (r *InMemory) DeleteInvitation(_ context.Context, workspaceID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, s := range r.invitations {
		if k.workspaceID == workspaceID && s.ID == id {
			delete(r.invitations, k)
			return nil
		}
	}
	return apperror.New(apperror.InvitationNotFound, "invitation not found")
}

func (r *InMemory) DeleteExpiredInvitations(_ context.Context, now time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for k, s := range r.invitations {
		if !now.Before(s.ExpiresAt) {
			delete(r.invitations, k)
			n++
		}
	}
	return n, nil
}
