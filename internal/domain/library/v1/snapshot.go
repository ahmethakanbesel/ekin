package library

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/pkg/timeutil"
)

// Snapshot is the persisted form of an Asset.
type Snapshot struct {
	ID, WorkspaceID, CreatedBy       uuid.UUID
	Kind, Origin, Status             string
	Title, Description               string
	Tags                             []string
	Mood                             string
	Tempo                            int
	Credit                           Credit
	Filename                         string
	Size                             int64
	SHA256, Format                   string
	Width, Height                    int
	Duration                         time.Duration
	Poster                           bool
	Failure                          string
	CreatedAt, UpdatedAt, ArchivedAt time.Time
}

// Snapshot returns a's persisted form.
func (a *Asset) Snapshot() Snapshot {
	return Snapshot{
		ID: a.id, WorkspaceID: a.workspaceID, CreatedBy: a.createdBy,
		Kind: string(a.kind), Origin: string(a.origin), Status: string(a.status),
		Title: a.title, Description: a.description, Tags: slices.Clone(a.tags), Mood: a.mood, Tempo: a.tempo,
		Credit: a.credit, Filename: a.filename, Size: a.size, SHA256: a.sha256, Format: a.format,
		Width: a.width, Height: a.height, Duration: a.duration, Poster: a.poster, Failure: a.failure,
		CreatedAt: a.createdAt, UpdatedAt: a.updatedAt, ArchivedAt: a.archivedAt,
	}
}

// Rehydrate rebuilds an Asset from storage.
func Rehydrate(s Snapshot) (*Asset, error) {
	kind, err := ParseKind(s.Kind)
	if err != nil {
		return nil, apperror.New(apperror.InternalError, "unknown asset kind")
	}
	origin, err := ParseOrigin(s.Origin)
	if err != nil {
		return nil, apperror.New(apperror.InternalError, "unknown asset origin")
	}
	status, err := ParseStatus(s.Status)
	if err != nil {
		return nil, apperror.New(apperror.InternalError, "unknown asset status")
	}
	if status == StatusReady && !slices.Contains(formats[kind], s.Format) {
		return nil, apperror.New(apperror.InternalError, "stored asset has an unknown format")
	}
	tags := slices.Clone(s.Tags)
	if tags == nil {
		tags = []string{}
	}
	return &Asset{
		id: s.ID, workspaceID: s.WorkspaceID, createdBy: s.CreatedBy,
		kind: kind, origin: origin, status: status,
		title: s.Title, description: s.Description, tags: tags, mood: s.Mood, tempo: s.Tempo,
		credit: s.Credit, filename: s.Filename, size: s.Size, sha256: s.SHA256, format: s.Format,
		width: s.Width, height: s.Height, duration: s.Duration, poster: s.Poster, failure: s.Failure,
		createdAt: timeutil.Normalize(s.CreatedAt), updatedAt: timeutil.Normalize(s.UpdatedAt),
		archivedAt: normalizeOptional(s.ArchivedAt),
	}, nil
}

func normalizeOptional(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return timeutil.Normalize(t)
}
