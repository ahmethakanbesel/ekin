// Package library models a workspace's media library: music, stock
// videos and photos kept for reuse, whether uploaded by a person, fetched
// from Pexels by the agent or composed by it.
package library

import (
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/pkg/fold"
	"kurgu/pkg/timeutil"
)

// Limits.
const (
	MaxTitleLength       = 200
	MaxDescriptionLength = 2000
	MaxMoodLength        = 100
	MaxCreditLength      = 300
	MaxTags              = 20
	MaxTagLength         = 40
	MaxTempo             = 300
	maxErrorLength       = 2000

	MaxVideoBytes = 300 << 20
	MaxPhotoBytes = 25 << 20
	MaxMusicBytes = 50 << 20
	// MaxUploadBytes is the largest file any kind accepts.
	MaxUploadBytes = MaxVideoBytes

	MaxVideoDuration = 2 * time.Minute
	MaxMusicDuration = 15 * time.Minute
)

// Kind is what an asset is.
type Kind string

const (
	KindMusic Kind = "music"
	KindVideo Kind = "video"
	KindPhoto Kind = "photo"
)

// Kinds lists every kind.
var Kinds = []Kind{KindMusic, KindVideo, KindPhoto}

// ParseKind parses a kind.
func ParseKind(s string) (Kind, error) {
	if k := Kind(s); slices.Contains(Kinds, k) {
		return k, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown asset kind")
}

// extensions are the upload types each kind accepts.
var extensions = map[Kind][]string{
	KindVideo: {".mp4", ".mov", ".m4v", ".webm"},
	KindPhoto: {".jpg", ".jpeg", ".png", ".webp"},
	KindMusic: {".mp3", ".wav", ".flac", ".m4a", ".aac", ".ogg", ".opus"},
}

// KindFromFilename picks the kind from the file extension.
func KindFromFilename(name string) (Kind, error) {
	ext := strings.ToLower(filepath.Ext(name))
	for _, k := range Kinds {
		if slices.Contains(extensions[k], ext) {
			return k, nil
		}
	}
	return "", apperror.New(apperror.ValidationFailed, "unsupported file type; upload MP4, MOV or WebM video, JPEG, PNG or WebP photos, or MP3, WAV, FLAC, M4A or Ogg music")
}

// MaxBytes is the largest upload of kind.
func (k Kind) MaxBytes() int64 {
	switch k {
	case KindVideo:
		return MaxVideoBytes
	case KindPhoto:
		return MaxPhotoBytes
	}
	return MaxMusicBytes
}

// Visual reports whether the kind has a picture (and so a size and an
// orientation).
func (k Kind) Visual() bool { return k == KindVideo || k == KindPhoto }

// formats are the stored main files each kind may have.
var formats = map[Kind][]string{
	KindVideo: {"webm"},
	KindPhoto: {"jpg"},
	KindMusic: {"opus", "js"},
}

// Origin is how an asset got into the library.
type Origin string

const (
	// OriginGenerated is a Tone.js score the agent composed.
	OriginGenerated Origin = "generated"
	// OriginPexels is a stock video or photo the agent fetched.
	OriginPexels Origin = "pexels"
	// OriginUpload is a file a person uploaded.
	OriginUpload Origin = "upload"
)

// ParseOrigin parses an origin.
func ParseOrigin(s string) (Origin, error) {
	switch o := Origin(s); o {
	case OriginGenerated, OriginPexels, OriginUpload:
		return o, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown asset origin")
}

// Status is where an asset is in ingestion.
type Status string

const (
	StatusProcessing Status = "processing"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
)

// ParseStatus parses an asset status.
func ParseStatus(s string) (Status, error) {
	switch st := Status(s); st {
	case StatusProcessing, StatusReady, StatusFailed:
		return st, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown asset status")
}

// Orientation is the shape of a picture.
type Orientation string

const (
	OrientationPortrait  Orientation = "portrait"
	OrientationLandscape Orientation = "landscape"
	OrientationSquare    Orientation = "square"
)

// ParseOrientation parses an orientation.
func ParseOrientation(s string) (Orientation, error) {
	switch o := Orientation(s); o {
	case OrientationPortrait, OrientationLandscape, OrientationSquare:
		return o, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown orientation")
}

// OrientationOf classifies a width × height picture; within 5% of
// square counts as square.
func OrientationOf(width, height int) Orientation {
	switch {
	case width <= 0 || height <= 0:
		return ""
	case float64(width) > float64(height)*1.05:
		return OrientationLandscape
	case float64(height) > float64(width)*1.05:
		return OrientationPortrait
	}
	return OrientationSquare
}

// Credit is the attribution a video using the asset gives. All empty
// means none is needed (the uploader's own work).
type Credit struct {
	Author  string `json:"author,omitempty"`
	URL     string `json:"url,omitempty"`
	Licence string `json:"licence,omitempty"`
}

// IsZero reports whether the credit is empty.
func (c Credit) IsZero() bool { return c == Credit{} }

// Details is what a person may edit about an asset.
type Details struct {
	Title       string
	Description string
	Tags        []string
	// Mood and Tempo (BPM, 0 for unknown) describe music only.
	Mood   string
	Tempo  int
	Credit Credit
}

// Upload describes a received file.
type Upload struct {
	Filename string
	Size     int64
	SHA256   string
}

// Media is what ingestion learned and produced: the main file's format
// (its extension), the picture's size, the length, and whether a poster
// frame was made.
type Media struct {
	Format   string
	Width    int
	Height   int
	Duration time.Duration
	Poster   bool
}

// Asset is one item in the library.
type Asset struct {
	id          uuid.UUID
	workspaceID uuid.UUID
	kind        Kind
	origin      Origin
	status      Status
	title       string
	description string
	tags        []string
	mood        string
	tempo       int
	credit      Credit
	filename    string
	size        int64
	sha256      string
	format      string
	width       int
	height      int
	duration    time.Duration
	poster      bool
	failure     string
	// provisional marks an asset saved from a draft whose deliverable is
	// not yet decided; kept, one a person saved or edited, which is never
	// pruned.
	provisional bool
	kept        bool
	harvest     Harvest
	createdBy   uuid.UUID
	createdAt   time.Time
	updatedAt   time.Time
	archivedAt  time.Time
}

// NewUpload registers an uploaded file; ingestion makes it ready later.
// An empty kind is taken from the file extension, and an empty title
// from the file name.
func NewUpload(id, workspaceID, createdBy uuid.UUID, kind Kind, up Upload, d Details, now time.Time) (*Asset, error) {
	if id == uuid.Nil || workspaceID == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "asset and workspace ids are required")
	}
	fileKind, err := KindFromFilename(up.Filename)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		kind = fileKind
	}
	if kind != fileKind {
		return nil, apperror.New(apperror.ValidationFailed, "the file is not a "+string(kind)+" file")
	}
	if up.Size <= 0 {
		return nil, apperror.New(apperror.ValidationFailed, "the file is empty")
	}
	if up.Size > kind.MaxBytes() {
		return nil, apperror.New(apperror.ValidationFailed, "the file is larger than "+strconv.FormatInt(kind.MaxBytes()>>20, 10)+" MB")
	}
	if up.SHA256 == "" {
		return nil, apperror.New(apperror.ValidationFailed, "the file hash is required")
	}
	if strings.TrimSpace(d.Title) == "" {
		d.Title = strings.TrimSuffix(filepath.Base(up.Filename), filepath.Ext(up.Filename))
	}
	now = timeutil.Normalize(now)
	a := &Asset{
		id: id, workspaceID: workspaceID, kind: kind, origin: OriginUpload, status: StatusProcessing,
		filename: filepath.Base(up.Filename), size: up.Size, sha256: up.SHA256,
		createdBy: createdBy, createdAt: now, updatedAt: now,
	}
	if err := a.Describe(d, now); err != nil {
		return nil, err
	}
	return a, nil
}

// Describe replaces the editable details.
func (a *Asset) Describe(d Details, now time.Time) error {
	title := strings.TrimSpace(d.Title)
	description := strings.TrimSpace(d.Description)
	mood := strings.TrimSpace(d.Mood)
	switch {
	case title == "":
		return apperror.New(apperror.ValidationFailed, "title is required")
	case utf8.RuneCountInString(title) > MaxTitleLength:
		return apperror.New(apperror.ValidationFailed, "title is too long")
	case utf8.RuneCountInString(description) > MaxDescriptionLength:
		return apperror.New(apperror.ValidationFailed, "description is too long")
	case utf8.RuneCountInString(mood) > MaxMoodLength:
		return apperror.New(apperror.ValidationFailed, "mood is too long")
	case a.kind != KindMusic && (mood != "" || d.Tempo != 0):
		return apperror.New(apperror.ValidationFailed, "only music has a mood and tempo")
	case d.Tempo < 0 || d.Tempo > MaxTempo:
		return apperror.New(apperror.ValidationFailed, "tempo must be between 0 and 300 BPM")
	}
	tags, err := normalizeTags(d.Tags)
	if err != nil {
		return err
	}
	credit, err := normalizeCredit(d.Credit)
	if err != nil {
		return err
	}
	a.title, a.description, a.tags, a.mood, a.tempo, a.credit = title, description, tags, mood, d.Tempo, credit
	// A person describing it wants it: it stays whatever happens to the
	// draft it came from.
	a.kept, a.provisional = true, false
	a.updatedAt = timeutil.Normalize(now)
	return nil
}

// normalizeTags trims, lowercases and de-duplicates tags, keeping their
// order.
func normalizeTags(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.Join(strings.Fields(t), " "))
		if t == "" || seen[fold.String(t)] {
			continue
		}
		if utf8.RuneCountInString(t) > MaxTagLength {
			return nil, apperror.New(apperror.ValidationFailed, "tag "+t+" is too long")
		}
		seen[fold.String(t)] = true
		out = append(out, t)
	}
	if len(out) > MaxTags {
		return nil, apperror.New(apperror.ValidationFailed, "at most 20 tags")
	}
	return out, nil
}

func normalizeCredit(c Credit) (Credit, error) {
	c = Credit{Author: strings.TrimSpace(c.Author), URL: strings.TrimSpace(c.URL), Licence: strings.TrimSpace(c.Licence)}
	for _, s := range []string{c.Author, c.URL, c.Licence} {
		if utf8.RuneCountInString(s) > MaxCreditLength {
			return c, apperror.New(apperror.ValidationFailed, "credit is too long")
		}
	}
	if c.URL != "" {
		u, err := url.Parse(c.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return c, apperror.New(apperror.ValidationFailed, "credit URL must be an http(s) link")
		}
	}
	return c, nil
}

// Harvest is where an asset saved from a draft came from.
type Harvest struct {
	StoryID       uuid.UUID
	DeliverableID uuid.UUID
	Revision      int
	// SourceRef identifies the outside item ("pexels:video:123"), so it is
	// saved once; SourceURL is its page, OriginalURL the file the stored
	// one was cut from. Empty for composed music.
	SourceRef   string
	SourceURL   string
	OriginalURL string
	// DerivedFrom is the library score a composed one was adapted from.
	DerivedFrom uuid.UUID
}

// Saved is a file a draft used, ready to keep: its hash and size, and
// what processing would have reported.
type Saved struct {
	SHA256 string
	Size   int64
	Media  Media
}

// NewHarvested keeps a draft's Pexels stock or composed score. Saved by
// the automatic harvest it is provisional until its deliverable is
// decided; saved by a person (kept) it is not.
func NewHarvested(id, workspaceID, createdBy uuid.UUID, kind Kind, origin Origin, h Harvest, f Saved, d Details,
	kept bool, now time.Time) (*Asset, error) {
	switch {
	case id == uuid.Nil || workspaceID == uuid.Nil:
		return nil, apperror.New(apperror.ValidationFailed, "asset and workspace ids are required")
	case h.StoryID == uuid.Nil || h.DeliverableID == uuid.Nil || h.Revision < 1:
		return nil, apperror.New(apperror.ValidationFailed, "a saved asset needs the revision it came from")
	case f.SHA256 == "" || f.Size <= 0:
		return nil, apperror.New(apperror.ValidationFailed, "a saved asset needs its file")
	case origin == OriginPexels && (kind == KindMusic || h.SourceRef == "" || d.Credit.Author == ""):
		return nil, apperror.New(apperror.ValidationFailed, "Pexels stock needs its id and author")
	case origin == OriginGenerated && (kind != KindMusic || h.SourceRef != ""):
		return nil, apperror.New(apperror.ValidationFailed, "only music is composed")
	case origin == OriginUpload:
		return nil, apperror.New(apperror.ValidationFailed, "uploads are not harvested")
	}
	if _, err := ParseOrigin(string(origin)); err != nil {
		return nil, err
	}
	now = timeutil.Normalize(now)
	a := &Asset{
		id: id, workspaceID: workspaceID, kind: kind, origin: origin, status: StatusProcessing,
		size: f.Size, sha256: f.SHA256, harvest: h, createdBy: createdBy, createdAt: now, updatedAt: now,
	}
	if err := a.Describe(d, now); err != nil {
		return nil, err
	}
	a.kept, a.provisional = kept, !kept
	if err := a.Ingested(f.Media, now); err != nil {
		return nil, err
	}
	if a.status != StatusReady {
		return nil, apperror.New(apperror.ValidationFailed, "the file cannot be kept: "+a.failure)
	}
	return a, nil
}

// Keep marks the asset wanted by a person: never pruned.
func (a *Asset) Keep(now time.Time) {
	if !a.kept || a.provisional {
		a.kept, a.provisional = true, false
		a.updatedAt = timeutil.Normalize(now)
	}
}

// Settle decides a provisional asset once its deliverable is decided: it
// stays when the decided revision uses it, a person kept it, or another
// video used it meanwhile, and is no longer provisional; otherwise it
// should be deleted (false). Assets that are not provisional always stay.
func (a *Asset) Settle(usedByDecided, usedElsewhere bool, now time.Time) bool {
	if !a.provisional {
		return true
	}
	if usedByDecided || a.kept || usedElsewhere {
		a.provisional = false
		a.updatedAt = timeutil.Normalize(now)
		return true
	}
	return false
}

// Ingested records the processed main file. A file past its kind's
// length limit fails the asset instead.
func (a *Asset) Ingested(m Media, now time.Time) error {
	if a.status != StatusProcessing {
		return apperror.New(apperror.InvalidTransition, "the asset is not being processed")
	}
	if !slices.Contains(formats[a.kind], m.Format) {
		return apperror.New(apperror.InternalError, "a "+string(a.kind)+" cannot be stored as "+m.Format)
	}
	if a.kind.Visual() && (m.Width <= 0 || m.Height <= 0) {
		return a.Failed("the file has no picture", now)
	}
	if a.kind != KindPhoto && m.Format != "js" && m.Duration <= 0 {
		return a.Failed("the file has no length", now)
	}
	switch {
	case a.kind == KindVideo && m.Duration > MaxVideoDuration+time.Second:
		return a.Failed("the video is longer than 2 minutes; trim it first", now)
	case a.kind == KindMusic && m.Duration > MaxMusicDuration+time.Second:
		return a.Failed("the track is longer than 15 minutes", now)
	}
	if a.kind == KindPhoto {
		m.Duration = 0
	}
	if !a.kind.Visual() {
		m.Width, m.Height, m.Poster = 0, 0, false
	}
	a.status, a.failure = StatusReady, ""
	a.format, a.width, a.height, a.duration, a.poster = m.Format, m.Width, m.Height, m.Duration.Round(time.Millisecond), m.Poster
	a.updatedAt = timeutil.Normalize(now)
	return nil
}

// Failed records an ingestion failure.
func (a *Asset) Failed(reason string, now time.Time) error {
	if len(reason) > maxErrorLength {
		reason = reason[:maxErrorLength]
	}
	a.status, a.failure = StatusFailed, reason
	a.updatedAt = timeutil.Normalize(now)
	return nil
}

// Retry puts a failed asset back to processing.
func (a *Asset) Retry(now time.Time) error {
	if a.status != StatusFailed {
		return apperror.New(apperror.InvalidTransition, "only a failed asset can be processed again")
	}
	a.status, a.failure = StatusProcessing, ""
	a.updatedAt = timeutil.Normalize(now)
	return nil
}

// Archive takes the asset out of new drafts' reach; it stays listed with
// its history.
func (a *Asset) Archive(now time.Time) {
	if a.archivedAt.IsZero() {
		now = timeutil.Normalize(now)
		a.archivedAt, a.updatedAt = now, now
	}
}

// Unarchive offers the asset to drafts again.
func (a *Asset) Unarchive(now time.Time) {
	if !a.archivedAt.IsZero() {
		a.archivedAt = time.Time{}
		a.updatedAt = timeutil.Normalize(now)
	}
}

// Usable reports whether a draft may use the asset.
func (a *Asset) Usable() bool { return a.status == StatusReady && a.archivedAt.IsZero() }

// Fits reports whether the asset suits a frame of orientation o: music
// fits any, a square picture fits any frame, others only their own.
func (a *Asset) Fits(o Orientation) bool {
	if !a.kind.Visual() {
		return true
	}
	own := a.Orientation()
	return own == o || own == OrientationSquare
}

// Orientation is the picture's shape; "" for music.
func (a *Asset) Orientation() Orientation { return OrientationOf(a.width, a.height) }

// File names an asset's stored file.
type File string

const (
	FileMain   File = "main"
	FilePoster File = "poster"
	// FilePreview is the start of a composed score, rendered to listen to.
	FilePreview File = "preview"
)

// ParseFile parses a file name.
func ParseFile(s string) (File, error) {
	switch f := File(s); f {
	case FileMain, FilePoster, FilePreview:
		return f, nil
	}
	return "", apperror.New(apperror.AssetNotFound, "no such file")
}

// Prefix is the blob store "directory" every file of the asset lives
// under.
func (a *Asset) Prefix() string {
	return "ws/" + a.workspaceID.String() + "/library/" + a.id.String()
}

// OriginalKey is where an upload waits for ingestion.
func (a *Asset) OriginalKey() string {
	return a.Prefix() + "/original" + strings.ToLower(filepath.Ext(a.filename))
}

// Key is where file f lives, and false when the asset has none.
func (a *Asset) Key(f File) (string, bool) {
	switch {
	case a.status != StatusReady:
		return "", false
	case f == FileMain:
		return a.Prefix() + "/main." + a.format, true
	case f == FilePoster && a.poster:
		return a.Prefix() + "/poster.jpg", true
	case f == FilePreview && a.format == "js":
		return a.Prefix() + "/preview.opus", true
	}
	return "", false
}

// ContentType is the media type file f is served with.
func (a *Asset) ContentType(f File) string {
	switch f {
	case FilePoster:
		return "image/jpeg"
	case FilePreview:
		return "audio/ogg"
	}
	switch a.format {
	case "webm":
		return "video/webm"
	case "jpg":
		return "image/jpeg"
	case "opus":
		return "audio/ogg"
	case "js":
		return "text/javascript; charset=utf-8"
	}
	return "application/octet-stream"
}

func (a *Asset) ID() uuid.UUID           { return a.id }
func (a *Asset) WorkspaceID() uuid.UUID  { return a.workspaceID }
func (a *Asset) Kind() Kind              { return a.kind }
func (a *Asset) Origin() Origin          { return a.origin }
func (a *Asset) Status() Status          { return a.status }
func (a *Asset) Title() string           { return a.title }
func (a *Asset) Description() string     { return a.description }
func (a *Asset) Tags() []string          { return slices.Clone(a.tags) }
func (a *Asset) Mood() string            { return a.mood }
func (a *Asset) Tempo() int              { return a.tempo }
func (a *Asset) Credit() Credit          { return a.credit }
func (a *Asset) Filename() string        { return a.filename }
func (a *Asset) Size() int64             { return a.size }
func (a *Asset) SHA256() string          { return a.sha256 }
func (a *Asset) Format() string          { return a.format }
func (a *Asset) Width() int              { return a.width }
func (a *Asset) Height() int             { return a.height }
func (a *Asset) Duration() time.Duration { return a.duration }
func (a *Asset) HasPoster() bool         { return a.poster }
func (a *Asset) Failure() string         { return a.failure }
func (a *Asset) CreatedBy() uuid.UUID    { return a.createdBy }
func (a *Asset) CreatedAt() time.Time    { return a.createdAt }
func (a *Asset) UpdatedAt() time.Time    { return a.updatedAt }
func (a *Asset) ArchivedAt() time.Time   { return a.archivedAt }
func (a *Asset) Provisional() bool       { return a.provisional }
func (a *Asset) Kept() bool              { return a.kept }
func (a *Asset) Harvest() Harvest        { return a.harvest }
