package identity

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

const (
	// MaxNameLength bounds a user's display name, in runes.
	MaxNameLength = 80
	// MinPasswordLength and MaxPasswordLength bound a new password, in
	// runes.
	MinPasswordLength = 8
	MaxPasswordLength = 128
	maxEmailLength    = 254
)

// Email is a normalised (trimmed, lower-cased) bare address.
type Email string

// ParseEmail parses a bare address such as "ada@example.com". Display
// names ("Ada <ada@example.com>") are rejected.
func ParseEmail(s string) (Email, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", apperror.New(apperror.ValidationFailed, "email is required")
	}
	if len(s) > maxEmailLength {
		return "", apperror.New(apperror.ValidationFailed, "email is too long")
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" {
		return "", apperror.New(apperror.ValidationFailed, "email is not a valid address")
	}
	return Email(s), nil
}

func (e Email) String() string { return string(e) }

// Password is a new password that meets the policy. It never prints.
type Password struct {
	plain string
}

// ParsePassword checks a new password against the policy: 8 to 128
// characters. Length beats composition rules (NIST SP 800-63B).
func ParsePassword(s string) (Password, error) {
	n := utf8.RuneCountInString(s)
	if n < MinPasswordLength {
		return Password{}, apperror.New(apperror.ValidationFailed, "password must be at least 8 characters")
	}
	if n > MaxPasswordLength {
		return Password{}, apperror.New(apperror.ValidationFailed, "password must be at most 128 characters")
	}
	if strings.TrimSpace(s) == "" {
		return Password{}, apperror.New(apperror.ValidationFailed, "password must not be blank")
	}
	return Password{plain: s}, nil
}

// Plain returns the password for hashing.
func (p Password) Plain() string { return p.plain }

func (Password) String() string   { return "[redacted]" }
func (Password) GoString() string { return "[redacted]" }

// User is a person who signs in. A user belongs to workspaces through
// memberships; the user itself carries no permissions.
type User struct {
	id           uuid.UUID
	email        Email
	name         string
	passwordHash string
	admin        bool // runs the instance: host profiles, settings
	emailNotify  bool // gets review digests by email
	createdAt    time.Time
	updatedAt    time.Time
}

// NewUser returns a user. passwordHash is the hash of a parsed Password.
func NewUser(id uuid.UUID, email Email, name, passwordHash string, now time.Time) (*User, error) {
	if id == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "user id is required")
	}
	if email == "" {
		return nil, apperror.New(apperror.ValidationFailed, "email is required")
	}
	u := &User{id: id, email: email, emailNotify: true, createdAt: timeutil.Normalize(now)}
	if err := u.Rename(name, now); err != nil {
		return nil, err
	}
	if err := u.SetPasswordHash(passwordHash, now); err != nil {
		return nil, err
	}
	u.updatedAt = u.createdAt
	return u, nil
}

// Rename changes the display name.
func (u *User) Rename(name string, now time.Time) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return apperror.New(apperror.ValidationFailed, "name is required")
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return apperror.New(apperror.ValidationFailed, "name is too long")
	}
	u.name = name
	u.updatedAt = timeutil.Normalize(now)
	return nil
}

// SetPasswordHash replaces the password. Callers revoke other sessions.
func (u *User) SetPasswordHash(hash string, now time.Time) error {
	if hash == "" {
		return apperror.New(apperror.ValidationFailed, "password hash is required")
	}
	u.passwordHash = hash
	u.updatedAt = timeutil.Normalize(now)
	return nil
}

// MakeAdmin grants instance administration.
func (u *User) MakeAdmin(now time.Time) {
	u.admin = true
	u.updatedAt = timeutil.Normalize(now)
}

// SetEmailNotifications turns review digests by email on or off.
func (u *User) SetEmailNotifications(on bool, now time.Time) {
	u.emailNotify = on
	u.updatedAt = timeutil.Normalize(now)
}

func (u *User) IsAdmin() bool            { return u.admin }
func (u *User) EmailNotifications() bool { return u.emailNotify }
func (u *User) ID() uuid.UUID            { return u.id }
func (u *User) Email() Email             { return u.email }
func (u *User) Name() string             { return u.name }
func (u *User) PasswordHash() string     { return u.passwordHash }
func (u *User) CreatedAt() time.Time     { return u.createdAt }
func (u *User) UpdatedAt() time.Time     { return u.updatedAt }

// UserSnapshot is the persisted form of a User.
type UserSnapshot struct {
	ID           uuid.UUID
	Email        string
	Name         string
	PasswordHash string
	Admin        bool
	EmailNotify  bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Snapshot returns u's persisted form.
func (u *User) Snapshot() UserSnapshot {
	return UserSnapshot{
		ID:           u.id,
		Email:        string(u.email),
		Name:         u.name,
		PasswordHash: u.passwordHash,
		Admin:        u.admin,
		EmailNotify:  u.emailNotify,
		CreatedAt:    u.createdAt,
		UpdatedAt:    u.updatedAt,
	}
}

// RehydrateUser rebuilds a User from storage.
func RehydrateUser(s UserSnapshot) (*User, error) {
	email, err := ParseEmail(s.Email)
	if err != nil {
		return nil, err
	}
	u, err := NewUser(s.ID, email, s.Name, s.PasswordHash, s.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.admin, u.emailNotify = s.Admin, s.EmailNotify
	u.updatedAt = timeutil.Normalize(s.UpdatedAt)
	return u, nil
}
