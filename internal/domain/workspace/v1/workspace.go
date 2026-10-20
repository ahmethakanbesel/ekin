// Package workspace defines the tenant every other aggregate belongs to.
// A workspace carries the default timezone the UI shows times in and
// pre-fills into new pipeline schedules.
package workspace

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// MaxNameLength bounds a workspace name, in runes.
const MaxNameLength = 80

// Workspace is a tenant.
type Workspace struct {
	id        uuid.UUID
	name      string
	location  *time.Location
	createdAt time.Time
	updatedAt time.Time
}

// New returns a workspace named name in timezone loc.
func New(id uuid.UUID, name string, loc *time.Location, now time.Time) (*Workspace, error) {
	if id == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "workspace id is required")
	}
	w := &Workspace{id: id, createdAt: timeutil.Normalize(now)}
	if err := w.Rename(name, now); err != nil {
		return nil, err
	}
	if err := w.ChangeTimezone(loc, now); err != nil {
		return nil, err
	}
	w.updatedAt = w.createdAt
	return w, nil
}

// Rename changes the display name.
func (w *Workspace) Rename(name string, now time.Time) error {
	name, err := parseName(name)
	if err != nil {
		return err
	}
	w.name = name
	w.updatedAt = timeutil.Normalize(now)
	return nil
}

// ChangeTimezone changes the default timezone. Existing pipeline
// schedules keep their own timezone; this only affects display and the
// default for new schedules.
func (w *Workspace) ChangeTimezone(loc *time.Location, now time.Time) error {
	if loc == nil {
		return apperror.New(apperror.ValidationFailed, "timezone is required")
	}
	w.location = loc
	w.updatedAt = timeutil.Normalize(now)
	return nil
}

func (w *Workspace) ID() uuid.UUID            { return w.id }
func (w *Workspace) Name() string             { return w.name }
func (w *Workspace) Location() *time.Location { return w.location }
func (w *Workspace) CreatedAt() time.Time     { return w.createdAt }
func (w *Workspace) UpdatedAt() time.Time     { return w.updatedAt }

// ParseTimezone parses an IANA timezone name such as "Europe/Istanbul".
// "Local" and the empty string are rejected: a stored schedule must not
// change meaning with the host it happens to run on.
func ParseTimezone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "Local" {
		return nil, apperror.New(apperror.ValidationFailed, "timezone must be an IANA name such as Europe/Istanbul")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, apperror.Wrap(apperror.ValidationFailed, err, "unknown timezone")
	}
	return loc, nil
}

func parseName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperror.New(apperror.ValidationFailed, "workspace name is required")
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", apperror.New(apperror.ValidationFailed, "workspace name is too long")
	}
	return name, nil
}

// Snapshot is the persisted form of a Workspace.
type Snapshot struct {
	ID        uuid.UUID
	Name      string
	Timezone  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Snapshot returns w's persisted form.
func (w *Workspace) Snapshot() Snapshot {
	return Snapshot{
		ID:        w.id,
		Name:      w.name,
		Timezone:  w.location.String(),
		CreatedAt: w.createdAt,
		UpdatedAt: w.updatedAt,
	}
}

// Rehydrate rebuilds a Workspace from storage, re-checking invariants.
func Rehydrate(s Snapshot) (*Workspace, error) {
	loc, err := ParseTimezone(s.Timezone)
	if err != nil {
		return nil, err
	}
	w, err := New(s.ID, s.Name, loc, s.CreatedAt)
	if err != nil {
		return nil, err
	}
	w.updatedAt = timeutil.Normalize(s.UpdatedAt)
	return w, nil
}
