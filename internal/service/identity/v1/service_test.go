package identity_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db/dbtest"
	identitydomain "ekin/internal/domain/identity/v1"
	workspacedomain "ekin/internal/domain/workspace/v1"
	identityrepo "ekin/internal/repository/identity/v1"
	workspacerepo "ekin/internal/repository/workspace/v1"
	identitysvc "ekin/internal/service/identity/v1"
	workspacesvc "ekin/internal/service/workspace/v1"
	"ekin/pkg/id"
	"ekin/pkg/password"
)

var t0 = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

type fixture struct {
	svc        *identitysvc.Service
	workspaces *workspacesvc.Service
	clock      *clock
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	_, tx := dbtest.Open(t)
	c := &clock{now: t0}
	repo := identityrepo.NewSQLite(tx)
	wsRepo := workspacerepo.NewSQLite(tx)
	hasher := password.New(password.Params{Memory: 64, Time: 1, Threads: 1})
	return fixture{
		svc:        identitysvc.NewService(repo, wsRepo, tx, hasher).WithClock(c.Now),
		workspaces: workspacesvc.NewService(wsRepo, repo, tx).WithClock(c.Now),
		clock:      c,
	}
}

func email(t *testing.T, s string) identitydomain.Email {
	t.Helper()
	e, err := identitydomain.ParseEmail(s)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func pw(t *testing.T, s string) identitydomain.Password {
	t.Helper()
	p, err := identitydomain.ParsePassword(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f fixture) user(t *testing.T, addr string) *identitydomain.User {
	t.Helper()
	u, err := f.svc.CreateUser(context.Background(), email(t, addr), "User", pw(t, "password-"+addr))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (f fixture) workspace(t *testing.T, owner *identitydomain.User) uuid.UUID {
	t.Helper()
	w, err := f.workspaces.Create(context.Background(), owner.ID(), "acme", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return w.ID()
}

func wantCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	if !apperror.Is(err, code) {
		t.Fatalf("err = %v, want %s", err, code)
	}
}

func TestLogin(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.user(t, "ada@example.com")

	in, err := f.svc.Login(ctx, email(t, "ada@example.com"), "password-ada@example.com")
	if err != nil || in.Token == "" || in.User.Email() != "ada@example.com" {
		t.Fatalf("login = %+v, %v", in, err)
	}
	_, err = f.svc.Login(ctx, email(t, "ada@example.com"), "wrong")
	wantCode(t, err, apperror.Unauthenticated)
	_, unknown := f.svc.Login(ctx, email(t, "nobody@example.com"), "wrong")
	if apperror.Message(err) != apperror.Message(unknown) {
		t.Fatalf("unknown email distinguishable: %q vs %q", apperror.Message(err), apperror.Message(unknown))
	}
}

func TestLoginLockout(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.user(t, "ada@example.com")
	ada := email(t, "ada@example.com")
	for range 9 { // plus one failure below = 10
		_, _ = f.svc.Login(ctx, ada, "wrong")
	}
	if _, err := f.svc.Login(ctx, ada, "password-ada@example.com"); err != nil {
		t.Fatalf("locked out early: %v", err)
	}
	// A success clears the count.
	for range 10 {
		_, _ = f.svc.Login(ctx, ada, "wrong")
	}
	_, err := f.svc.Login(ctx, ada, "password-ada@example.com")
	wantCode(t, err, apperror.TooManyAttempts)
	// Unknown emails lock too, so probing does not reveal accounts.
	for range 10 {
		_, _ = f.svc.Login(ctx, email(t, "ghost@example.com"), "wrong")
	}
	_, err = f.svc.Login(ctx, email(t, "ghost@example.com"), "wrong")
	wantCode(t, err, apperror.TooManyAttempts)

	f.clock.Advance(15 * time.Minute)
	if _, err := f.svc.Login(ctx, ada, "password-ada@example.com"); err != nil {
		t.Fatalf("still locked after the window: %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.user(t, "ada@example.com")
	in, _ := f.svc.Login(ctx, email(t, "ada@example.com"), "password-ada@example.com")

	u, sess, err := f.svc.Authenticate(ctx, in.Token)
	if err != nil || u.ID() != in.User.ID() || sess.ID() != in.Session.ID() {
		t.Fatalf("authenticate = %v, %v", u, err)
	}
	_, _, err = f.svc.Authenticate(ctx, in.Session.ID()) // the stored hash is not a token
	wantCode(t, err, apperror.Unauthenticated)
	_, _, err = f.svc.Authenticate(ctx, "")
	wantCode(t, err, apperror.Unauthenticated)

	// Used every 20 days, the session outlives the 30-day idle window.
	for range 2 {
		f.clock.Advance(20 * 24 * time.Hour)
		if _, _, err := f.svc.Authenticate(ctx, in.Token); err != nil {
			t.Fatalf("active session rejected: %v", err)
		}
	}
	f.clock.Advance(identitydomain.SessionIdle)
	_, _, err = f.svc.Authenticate(ctx, in.Token)
	wantCode(t, err, apperror.Unauthenticated)

	again, _ := f.svc.Login(ctx, email(t, "ada@example.com"), "password-ada@example.com")
	if err := f.svc.Logout(ctx, again.Session.ID()); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.svc.Authenticate(ctx, again.Token)
	wantCode(t, err, apperror.Unauthenticated)
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	u := f.user(t, "ada@example.com")
	ada := email(t, "ada@example.com")
	here, _ := f.svc.Login(ctx, ada, "password-ada@example.com")
	there, _ := f.svc.Login(ctx, ada, "password-ada@example.com")

	err := f.svc.ChangePassword(ctx, u.ID(), here.Session.ID(), "wrong", pw(t, "a-new-password"))
	wantCode(t, err, apperror.ValidationFailed)

	if err := f.svc.ChangePassword(ctx, u.ID(), here.Session.ID(), "password-ada@example.com", pw(t, "a-new-password")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Authenticate(ctx, here.Token); err != nil {
		t.Fatalf("current session signed out: %v", err)
	}
	_, _, err = f.svc.Authenticate(ctx, there.Token)
	wantCode(t, err, apperror.Unauthenticated)
	_, err = f.svc.Login(ctx, ada, "password-ada@example.com")
	wantCode(t, err, apperror.Unauthenticated)
	if _, err := f.svc.Login(ctx, ada, "a-new-password"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}

	if err := f.svc.ResetPassword(ctx, ada, pw(t, "reset-by-cli")); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.svc.Authenticate(ctx, here.Token)
	wantCode(t, err, apperror.Unauthenticated)
}

func TestInvitationFlow(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	owner := f.user(t, "owner@example.com")
	ws := f.workspace(t, owner)

	inv, token, err := f.svc.Invite(ctx, ws, owner.ID(), email(t, "New@Example.com"), identitydomain.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.svc.PreviewInvitation(ctx, token)
	if err != nil || preview.AccountExists || preview.WorkspaceName != "acme" || preview.Invitation.ID() != inv.ID() {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	_, err = f.svc.AcceptInvitation(ctx, token, "Newcomer", "short")
	wantCode(t, err, apperror.ValidationFailed)

	got, err := f.svc.AcceptInvitation(ctx, token, "Newcomer", "a-good-password")
	if err != nil || got.WorkspaceID != ws || got.User.Name() != "Newcomer" || got.Token == "" {
		t.Fatalf("accept = %+v, %v", got, err)
	}
	if _, _, err := f.svc.Authenticate(ctx, got.Token); err != nil {
		t.Fatalf("accept did not sign in: %v", err)
	}
	m, err := f.svc.Authorize(ctx, got.User.ID(), ws, identitydomain.ActionEdit)
	if err != nil || m.Role() != identitydomain.RoleEditor {
		t.Fatalf("membership = %v, %v", m, err)
	}
	_, err = f.svc.AcceptInvitation(ctx, token, "Again", "a-good-password")
	wantCode(t, err, apperror.InvitationNotFound)

	_, _, err = f.svc.Invite(ctx, ws, owner.ID(), email(t, "new@example.com"), identitydomain.RoleViewer)
	wantCode(t, err, apperror.AlreadyMember)
	_, _, err = f.svc.Invite(ctx, ws, got.User.ID(), email(t, "other@example.com"), identitydomain.RoleViewer)
	wantCode(t, err, apperror.Forbidden)
}

func TestAcceptWithExistingAccount(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	owner := f.user(t, "owner@example.com")
	ws := f.workspace(t, owner)
	f.user(t, "ada@example.com")

	_, token, _ := f.svc.Invite(ctx, ws, owner.ID(), email(t, "ada@example.com"), identitydomain.RoleViewer)
	preview, _ := f.svc.PreviewInvitation(ctx, token)
	if !preview.AccountExists {
		t.Fatal("existing account not reported")
	}
	_, err := f.svc.AcceptInvitation(ctx, token, "", "wrong")
	wantCode(t, err, apperror.Unauthenticated)
	got, err := f.svc.AcceptInvitation(ctx, token, "", "password-ada@example.com")
	if err != nil || got.User.Name() != "User" {
		t.Fatalf("accept = %+v, %v", got, err)
	}
	_, err = f.svc.Authorize(ctx, got.User.ID(), ws, identitydomain.ActionEdit)
	wantCode(t, err, apperror.Forbidden)
}

func TestInvitationRevokeAndExpiry(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	owner := f.user(t, "owner@example.com")
	ws := f.workspace(t, owner)

	revoked, token, _ := f.svc.Invite(ctx, ws, owner.ID(), email(t, "a@example.com"), identitydomain.RoleViewer)
	if err := f.svc.RevokeInvitation(ctx, ws, revoked.ID()); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.PreviewInvitation(ctx, token)
	wantCode(t, err, apperror.InvitationNotFound)

	_, token, _ = f.svc.Invite(ctx, ws, owner.ID(), email(t, "b@example.com"), identitydomain.RoleViewer)
	f.clock.Advance(identitydomain.InvitationTTL)
	_, err = f.svc.AcceptInvitation(ctx, token, "B", "a-good-password")
	wantCode(t, err, apperror.InvitationNotFound)
	if pending, _ := f.svc.Invitations(ctx, ws); len(pending) != 0 {
		t.Fatalf("expired invitation listed: %d", len(pending))
	}
	if _, n, err := f.svc.Sweep(ctx); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
}

func TestMembership(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	owner := f.user(t, "owner@example.com")
	ws := f.workspace(t, owner)
	_, token, _ := f.svc.Invite(ctx, ws, owner.ID(), email(t, "ed@example.com"), identitydomain.RoleEditor)
	ed, _ := f.svc.AcceptInvitation(ctx, token, "Ed", "a-good-password")

	_, err := f.svc.Authorize(ctx, f.user(t, "stranger@example.com").ID(), ws, identitydomain.ActionView)
	wantCode(t, err, apperror.WorkspaceNotFound)

	_, err = f.svc.ChangeRole(ctx, ws, owner.ID(), owner.ID(), identitydomain.RoleEditor)
	wantCode(t, err, apperror.LastOwner)
	err = f.svc.RemoveMember(ctx, ws, owner.ID(), owner.ID())
	wantCode(t, err, apperror.LastOwner)
	_, err = f.svc.ChangeRole(ctx, ws, ed.User.ID(), ed.User.ID(), identitydomain.RoleOwner)
	wantCode(t, err, apperror.Forbidden)

	promoted, err := f.svc.ChangeRole(ctx, ws, owner.ID(), ed.User.ID(), identitydomain.RoleOwner)
	if err != nil || promoted.Membership.Role() != identitydomain.RoleOwner || promoted.User.Name() != "Ed" {
		t.Fatalf("promote = %+v, %v", promoted, err)
	}
	if err := f.svc.RemoveMember(ctx, ws, owner.ID(), owner.ID()); err != nil {
		t.Fatalf("owner could not leave with a co-owner: %v", err)
	}
	members, _ := f.svc.Members(ctx, ws)
	if len(members) != 1 || members[0].User.ID() != ed.User.ID() {
		t.Fatalf("members = %d", len(members))
	}
}

func TestWorkspacesForUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ada := f.user(t, "ada@example.com")
	bob := f.user(t, "bob@example.com")
	f.workspace(t, ada)
	f.clock.Advance(time.Minute)
	f.workspace(t, bob)

	entries, err := f.workspaces.ListFor(ctx, ada.ID())
	if err != nil || len(entries) != 1 || entries[0].Role != identitydomain.RoleOwner {
		t.Fatalf("ada's workspaces = %+v, %v", entries, err)
	}
}

func TestAdoptOrphans(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	wsRepo := workspacerepo.NewSQLite(tx)
	repo := identityrepo.NewSQLite(tx)
	orphan, _ := workspacedomain.New(id.New(), "from milestone 1", time.UTC, t0)
	if err := wsRepo.Create(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	svc := identitysvc.NewService(repo, wsRepo, tx, password.New(password.Params{Memory: 64, Time: 1, Threads: 1}))
	workspaces := workspacesvc.NewService(wsRepo, repo, tx)
	ada, _ := svc.CreateUser(ctx, email(t, "ada@example.com"), "Ada", pw(t, "password-ada"))
	bob, _ := svc.CreateUser(ctx, email(t, "bob@example.com"), "Bob", pw(t, "password-bob"))
	if _, err := workspaces.Create(ctx, bob.ID(), "bob's", time.UTC); err != nil {
		t.Fatal(err)
	}

	n, err := workspaces.AdoptOrphans(ctx, ada.ID())
	if err != nil || n != 1 {
		t.Fatalf("adopted = %d, %v", n, err)
	}
	if n, _ := workspaces.AdoptOrphans(ctx, ada.ID()); n != 0 {
		t.Fatalf("adopted twice: %d", n)
	}
	entries, _ := workspaces.ListFor(ctx, ada.ID())
	if len(entries) != 1 || entries[0].Workspace.ID() != orphan.ID() || entries[0].Role != identitydomain.RoleOwner {
		t.Fatalf("ada's workspaces = %+v", entries)
	}
}

// racingUsers runs race once, right after the next CountUsers.
type racingUsers struct {
	identityrepo.Repository
	race func()
}

func (r *racingUsers) CountUsers(ctx context.Context) (int, error) {
	n, err := r.Repository.CountUsers(ctx)
	if race := r.race; race != nil {
		r.race = nil
		race()
	}
	return n, err
}

// Of two first accounts created at once, only one runs the instance.
func TestOnlyOneFirstAccountIsAdmin(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	repo := &racingUsers{Repository: identityrepo.NewSQLite(tx)}
	hasher := password.New(password.Params{Memory: 64, Time: 1, Threads: 1})
	svc := identitysvc.NewService(repo, workspacerepo.NewSQLite(tx), tx, hasher)
	other := make(chan *identitydomain.User, 1)
	repo.race = func() {
		go func() {
			u, err := svc.CreateUser(context.Background(), email(t, "b@example.com"), "B", pw(t, "password-b-1234"))
			if err != nil {
				t.Error(err)
			}
			other <- u
		}()
		select {
		case u := <-other:
			other <- u
		case <-time.After(300 * time.Millisecond):
		}
	}
	a, err := svc.CreateUser(ctx, email(t, "a@example.com"), "A", pw(t, "password-a-1234"))
	if err != nil {
		t.Fatal(err)
	}
	b := <-other
	if b == nil {
		t.Fatal("the second account was not created")
	}
	if a.IsAdmin() == b.IsAdmin() {
		t.Fatalf("admins: a %v, b %v; want exactly one", a.IsAdmin(), b.IsAdmin())
	}
}

// Guesses sent in parallel get no more tries than sequential ones: the
// lock counts an attempt before checking its password.
func TestParallelGuessesHitTheLimit(t *testing.T) {
	const guesses, limit = 40, 10
	tests := []struct {
		name  string
		email string
	}{
		{name: "an account", email: "a@example.com"},
		{name: "no account", email: "ghost@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.user(t, "a@example.com")
			var wg sync.WaitGroup
			var mu sync.Mutex
			checked := 0
			for range guesses {
				wg.Go(func() {
					_, err := f.svc.Login(context.Background(), email(t, tt.email), "wrong-password-1234")
					if apperror.Is(err, apperror.Unauthenticated) {
						mu.Lock()
						checked++
						mu.Unlock()
					} else if !apperror.Is(err, apperror.TooManyAttempts) {
						t.Errorf("login = %v", err)
					}
				})
			}
			wg.Wait()
			if checked != limit {
				t.Fatalf("%d of %d parallel guesses were checked, want %d", checked, guesses, limit)
			}
		})
	}
}
