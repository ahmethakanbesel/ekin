// Package knowledge models the knowledge base: uploaded documents split
// into pages of extracted text, each with a token estimate, so pipelines
// can hand an agent exactly the pages they reference.
package knowledge

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/pkg/timeutil"
)

// Limits.
const (
	MaxTitleLength = 200
	MaxFileBytes   = 100 << 20
	MaxPages       = 5000
	maxErrorLength = 2000
)

// Format is a supported upload type.
type Format string

const (
	FormatPDF      Format = "pdf"
	FormatText     Format = "txt"
	FormatMarkdown Format = "md"
	FormatEPUB     Format = "epub"
)

// FormatFromFilename picks the format from the file extension.
func FormatFromFilename(name string) (Format, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return FormatPDF, nil
	case ".txt", ".text":
		return FormatText, nil
	case ".md", ".markdown":
		return FormatMarkdown, nil
	case ".epub":
		return FormatEPUB, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unsupported file type; upload PDF, TXT, MD or EPUB")
}

// ParseFormat parses a stored format.
func ParseFormat(s string) (Format, error) {
	switch f := Format(s); f {
	case FormatPDF, FormatText, FormatMarkdown, FormatEPUB:
		return f, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown document format")
}

// ContentType is the MIME type the original file is served with.
func (f Format) ContentType() string {
	switch f {
	case FormatPDF:
		return "application/pdf"
	case FormatMarkdown:
		return "text/markdown; charset=utf-8"
	case FormatEPUB:
		return "application/epub+zip"
	}
	return "text/plain; charset=utf-8"
}

// Status is where a document is in extraction.
type Status string

const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

// Document is an uploaded file and the summary of its extracted pages.
type Document struct {
	id          uuid.UUID
	workspaceID uuid.UUID
	title       string
	filename    string
	format      Format
	size        int64
	sha256      string
	status      Status
	pageCount   int
	tokens      int
	failure     string
	createdBy   uuid.UUID
	createdAt   time.Time
	updatedAt   time.Time
}

// Upload describes a received file.
type Upload struct {
	Filename string
	Size     int64
	SHA256   string
}

// New registers an upload; extraction fills in the pages later.
func New(id, workspaceID, createdBy uuid.UUID, title string, up Upload, now time.Time) (*Document, error) {
	if id == uuid.Nil || workspaceID == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "document and workspace ids are required")
	}
	format, err := FormatFromFilename(up.Filename)
	if err != nil {
		return nil, err
	}
	if up.Size <= 0 {
		return nil, apperror.New(apperror.ValidationFailed, "the file is empty")
	}
	if up.Size > MaxFileBytes {
		return nil, apperror.New(apperror.ValidationFailed, "the file is larger than 100 MB")
	}
	if strings.TrimSpace(title) == "" {
		title = strings.TrimSuffix(filepath.Base(up.Filename), filepath.Ext(up.Filename))
	}
	now = timeutil.Normalize(now)
	d := &Document{
		id:          id,
		workspaceID: workspaceID,
		filename:    filepath.Base(up.Filename),
		format:      format,
		size:        up.Size,
		sha256:      up.SHA256,
		status:      StatusPending,
		createdBy:   createdBy,
		createdAt:   now,
		updatedAt:   now,
	}
	if err := d.Rename(title, now); err != nil {
		return nil, err
	}
	return d, nil
}

// Rename changes the title.
func (d *Document) Rename(title string, now time.Time) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return apperror.New(apperror.ValidationFailed, "title is required")
	}
	if utf8.RuneCountInString(title) > MaxTitleLength {
		return apperror.New(apperror.ValidationFailed, "title is too long")
	}
	d.title = title
	d.updatedAt = timeutil.Normalize(now)
	return nil
}

// Extracted records a successful extraction of pages.
func (d *Document) Extracted(pages []Page, now time.Time) error {
	if len(pages) == 0 {
		return d.Failed("no text could be extracted (is it a scanned PDF?)", now)
	}
	if len(pages) > MaxPages {
		return d.Failed("the document has more than 5000 pages", now)
	}
	total := 0
	for i, p := range pages {
		if p.Number != i+1 {
			return apperror.New(apperror.InternalError, "pages must be numbered from 1 without gaps")
		}
		total += p.Tokens
	}
	d.status, d.pageCount, d.tokens, d.failure = StatusReady, len(pages), total, ""
	d.updatedAt = timeutil.Normalize(now)
	return nil
}

// Failed records an extraction failure.
func (d *Document) Failed(reason string, now time.Time) error {
	if len(reason) > maxErrorLength {
		reason = reason[:maxErrorLength]
	}
	d.status, d.pageCount, d.tokens, d.failure = StatusFailed, 0, 0, reason
	d.updatedAt = timeutil.Normalize(now)
	return nil
}

// Retry puts a failed or ready document back to pending, to extract it
// again (a ready one keeps its pages until the new ones replace them).
func (d *Document) Retry(now time.Time) error {
	if d.status == StatusPending {
		return apperror.New(apperror.InvalidTransition, "the document is already being extracted")
	}
	d.status, d.failure = StatusPending, ""
	d.updatedAt = timeutil.Normalize(now)
	return nil
}

// CheckRange verifies a reference's page range against this document.
// A zero range means the whole document.
func (d *Document) CheckRange(from, to int) error {
	if d.status != StatusReady {
		return apperror.New(apperror.ValidationFailed, "document "+d.title+" is not ready yet")
	}
	if from == 0 && to == 0 {
		return nil
	}
	if from < 1 || to < from {
		return apperror.New(apperror.ValidationFailed, "page range is invalid")
	}
	if to > d.pageCount {
		return apperror.New(apperror.ValidationFailed, "document "+d.title+" has only "+strconv.Itoa(d.pageCount)+" pages")
	}
	return nil
}

// BlobKey is where the original file lives in the blob store.
func (d *Document) BlobKey() string {
	return "ws/" + d.workspaceID.String() + "/documents/" + d.id.String() + "/original." + string(d.format)
}

func (d *Document) ID() uuid.UUID          { return d.id }
func (d *Document) WorkspaceID() uuid.UUID { return d.workspaceID }
func (d *Document) Title() string          { return d.title }
func (d *Document) Filename() string       { return d.filename }
func (d *Document) Format() Format         { return d.format }
func (d *Document) Size() int64            { return d.size }
func (d *Document) SHA256() string         { return d.sha256 }
func (d *Document) Status() Status         { return d.status }
func (d *Document) PageCount() int         { return d.pageCount }
func (d *Document) Tokens() int            { return d.tokens }
func (d *Document) Failure() string        { return d.failure }
func (d *Document) CreatedBy() uuid.UUID   { return d.createdBy }
func (d *Document) CreatedAt() time.Time   { return d.createdAt }
func (d *Document) UpdatedAt() time.Time   { return d.updatedAt }

// Snapshot is the persisted form of a Document.
type Snapshot struct {
	ID, WorkspaceID, CreatedBy uuid.UUID
	Title, Filename, Format    string
	Size                       int64
	SHA256, Status, Failure    string
	PageCount, Tokens          int
	CreatedAt, UpdatedAt       time.Time
}

// Snapshot returns d's persisted form.
func (d *Document) Snapshot() Snapshot {
	return Snapshot{
		ID: d.id, WorkspaceID: d.workspaceID, CreatedBy: d.createdBy,
		Title: d.title, Filename: d.filename, Format: string(d.format),
		Size: d.size, SHA256: d.sha256, Status: string(d.status), Failure: d.failure,
		PageCount: d.pageCount, Tokens: d.tokens,
		CreatedAt: d.createdAt, UpdatedAt: d.updatedAt,
	}
}

// Rehydrate rebuilds a Document from storage.
func Rehydrate(s Snapshot) (*Document, error) {
	format, err := ParseFormat(s.Format)
	if err != nil {
		return nil, err
	}
	status := Status(s.Status)
	switch status {
	case StatusPending, StatusReady, StatusFailed:
	default:
		return nil, apperror.New(apperror.InternalError, "unknown document status")
	}
	return &Document{
		id: s.ID, workspaceID: s.WorkspaceID, createdBy: s.CreatedBy,
		title: s.Title, filename: s.Filename, format: format,
		size: s.Size, sha256: s.SHA256, status: status, failure: s.Failure,
		pageCount: s.PageCount, tokens: s.Tokens,
		createdAt: timeutil.Normalize(s.CreatedAt), updatedAt: timeutil.Normalize(s.UpdatedAt),
	}, nil
}
