package identity

import (
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

const (
	// SessionIdle ends a session nobody used for this long.
	SessionIdle = 30 * 24 * time.Hour
	// SessionLifetime ends every session this long after sign-in, used or
	// not.
	SessionLifetime = 90 * 24 * time.Hour
	// sessionTouchEvery limits last-seen writes to one per hour per
	// session, so reads stay reads.
	sessionTouchEvery = time.Hour
)

// Session is a signed-in browser. Its id is the hash of the cookie token.
type Session struct {
	id         string
	userID     uuid.UUID
	createdAt  time.Time
	lastSeenAt time.Time
	expiresAt  time.Time
}

// NewSession signs userID in and returns the session with the token to
// hand to the browser. The token is not stored.
func NewSession(userID uuid.UUID, now time.Time) (*Session, string) {
	token, hash := newToken()
	now = timeutil.Normalize(now)
	s := &Session{id: hash, userID: userID, createdAt: now, lastSeenAt: now}
	s.expiresAt = s.deadline(now)
	return s, token
}

// Active reports whether s still authenticates at now.
func (s *Session) Active(now time.Time) bool {
	return now.Before(s.expiresAt)
}

// Touch records use at now and slides the idle expiry. It reports whether
// anything changed, so callers write at most once per hour.
func (s *Session) Touch(now time.Time) bool {
	if !s.Active(now) || now.Sub(s.lastSeenAt) < sessionTouchEvery {
		return false
	}
	s.lastSeenAt = timeutil.Normalize(now)
	s.expiresAt = s.deadline(s.lastSeenAt)
	return true
}

func (s *Session) deadline(lastSeen time.Time) time.Time {
	idle := lastSeen.Add(SessionIdle)
	if hard := s.createdAt.Add(SessionLifetime); hard.Before(idle) {
		return hard
	}
	return idle
}

func (s *Session) ID() string            { return s.id }
func (s *Session) UserID() uuid.UUID     { return s.userID }
func (s *Session) CreatedAt() time.Time  { return s.createdAt }
func (s *Session) LastSeenAt() time.Time { return s.lastSeenAt }
func (s *Session) ExpiresAt() time.Time  { return s.expiresAt }

// SessionSnapshot is the persisted form of a Session.
type SessionSnapshot struct {
	ID         string
	UserID     uuid.UUID
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// Snapshot returns s's persisted form.
func (s *Session) Snapshot() SessionSnapshot {
	return SessionSnapshot{
		ID:         s.id,
		UserID:     s.userID,
		CreatedAt:  s.createdAt,
		LastSeenAt: s.lastSeenAt,
		ExpiresAt:  s.expiresAt,
	}
}

// RehydrateSession rebuilds a Session from storage.
func RehydrateSession(s SessionSnapshot) (*Session, error) {
	if s.ID == "" || s.UserID == uuid.Nil {
		return nil, apperror.New(apperror.InternalError, "stored session is incomplete")
	}
	return &Session{
		id:         s.ID,
		userID:     s.UserID,
		createdAt:  timeutil.Normalize(s.CreatedAt),
		lastSeenAt: timeutil.Normalize(s.LastSeenAt),
		expiresAt:  timeutil.Normalize(s.ExpiresAt),
	}, nil
}
