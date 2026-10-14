// Package story models one idea on its way to publication: the story
// owns its deliverables (a video, an article, translations), each moving
// through drafting, rendering and human review, with every agent pass
// recorded as a numbered revision.
package story

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	"kurgu/pkg/timeutil"
)

// Origin says what created a story.
type Origin string

const (
	OriginSchedule Origin = "schedule"
	OriginManual   Origin = "manual"
	OriginAdHoc    Origin = "adhoc"
)

// ParseOrigin parses a stored origin.
func ParseOrigin(s string) (Origin, error) {
	switch o := Origin(s); o {
	case OriginSchedule, OriginManual, OriginAdHoc:
		return o, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "unknown story origin")
}

// Limits.
const (
	MaxTitleLength   = 200
	MaxAngleLength   = 2000
	MaxKeywords      = 20
	MaxFeedbackRunes = 5000
)

// Idea is what a story is about.
type Idea struct {
	Title    string
	Angle    string   // the specific take, a few sentences
	Keywords []string // SEO keywords or tags
}

// NewIdea checks and normalises an idea.
func NewIdea(title, angle string, keywords []string) (Idea, error) {
	title = strings.Join(strings.Fields(title), " ")
	angle = strings.TrimSpace(angle)
	if title == "" {
		return Idea{}, apperror.New(apperror.ValidationFailed, "idea title is required")
	}
	if utf8.RuneCountInString(title) > MaxTitleLength {
		return Idea{}, apperror.New(apperror.ValidationFailed, "idea title is too long")
	}
	if utf8.RuneCountInString(angle) > MaxAngleLength {
		return Idea{}, apperror.New(apperror.ValidationFailed, "idea angle is too long")
	}
	var kws []string
	seen := map[string]bool{}
	for _, k := range keywords {
		k = strings.TrimSpace(k)
		if k == "" || seen[strings.ToLower(k)] {
			continue
		}
		seen[strings.ToLower(k)] = true
		kws = append(kws, k)
		if len(kws) == MaxKeywords {
			break
		}
	}
	return Idea{Title: title, Angle: angle, Keywords: kws}, nil
}

// Fingerprint is a normalised form of the title for duplicate checks:
// lower-cased (Turkish-aware), accents folded, punctuation dropped.
func (i Idea) Fingerprint() string {
	return Fingerprint(i.Title)
}

// Fingerprint normalises text for similarity comparisons.
func Fingerprint(s string) string {
	s = strings.NewReplacer("İ", "i", "I", "ı").Replace(s)
	s = strings.ToLower(s)
	s = norm.NFD.String(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
			// drop combining marks: â → a, ç → c, ş → s
		case r == 'ı':
			b.WriteRune('i')
			space = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		default:
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// Ref is a page window of a knowledge document a story draws on.
type Ref struct {
	DocumentID uuid.UUID `json:"documentId"`
	From       int       `json:"from,omitempty"`
	To         int       `json:"to,omitempty"`
}

// Brief is everything the agent is told beyond the idea.
type Brief struct {
	Instructions string `json:"instructions"`
	Style        string `json:"style,omitempty"`
	Refs         []Ref  `json:"refs,omitempty"`
	// Template is the house style chosen for the story, copied at
	// planning so later template edits leave it alone.
	Template *TemplateStyle `json:"template,omitempty"`
	// Keyword is the search term an SEO-sourced story targets.
	Keyword       string    `json:"keyword,omitempty"`
	OpportunityID uuid.UUID `json:"opportunityId,omitempty"`
}

// TemplateStyle is a copy of a template's style.
type TemplateStyle struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Guide       string    `json:"guide"`
	Palette     []string  `json:"palette,omitempty"`
	HeadingFont string    `json:"headingFont,omitempty"`
	BodyFont    string    `json:"bodyFont,omitempty"`
	MusicMood   string    `json:"musicMood,omitempty"`
	Kinds       []string  `json:"kinds,omitempty"`
}

// Applies reports whether the style covers a deliverable of kind.
func (t *TemplateStyle) Applies(kind string) bool {
	if t == nil {
		return false
	}
	for _, k := range t.Kinds {
		if k == kind {
			return true
		}
	}
	return len(t.Kinds) == 0
}

// Story is the aggregate root.
type Story struct {
	id           uuid.UUID
	workspaceID  uuid.UUID
	pipelineID   uuid.UUID // Nil for ad-hoc stories
	origin       Origin
	language     pipelinedomain.Language
	idea         Idea
	brief        Brief
	deliverables []*Deliverable
	createdBy    uuid.UUID
	createdAt    time.Time
	updatedAt    time.Time
	version      int64
}

// Plan is the input for a new story.
type Plan struct {
	WorkspaceID uuid.UUID
	PipelineID  uuid.UUID
	Origin      Origin
	Language    pipelinedomain.Language
	Idea        Idea
	Brief       Brief
	Outputs     pipelinedomain.Outputs
	CreatedBy   uuid.UUID
}

// New creates a story with one deliverable per output. The lead is
// queued; a derived deliverable waits for the lead's approval, because it
// is built from (and links to) the approved, published lead.
func New(id uuid.UUID, p Plan, ids func() uuid.UUID, now time.Time) (*Story, error) {
	if id == uuid.Nil || p.WorkspaceID == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "story and workspace ids are required")
	}
	if _, err := pipelinedomain.ParseLanguage(string(p.Language)); err != nil {
		return nil, err
	}
	if _, err := ParseOrigin(string(p.Origin)); err != nil {
		return nil, err
	}
	if p.Idea.Title == "" {
		return nil, apperror.New(apperror.ValidationFailed, "idea title is required")
	}
	now = timeutil.Normalize(now)
	s := &Story{
		id: id, workspaceID: p.WorkspaceID, pipelineID: p.PipelineID, origin: p.Origin,
		language: p.Language, idea: p.Idea, brief: p.Brief, createdBy: p.CreatedBy,
		createdAt: now, updatedAt: now, version: 1,
	}
	lead := p.Outputs.Lead()
	add := func(kind pipelinedomain.Kind) *Deliverable {
		d := &Deliverable{
			id: ids(), kind: kind, language: p.Language, status: StatusQueued,
			createdAt: now, updatedAt: now,
		}
		if v, ok := p.Outputs.Video(); ok && kind == pipelinedomain.KindVideo {
			d.video = &v
		}
		if a, ok := p.Outputs.Article(); ok && kind == pipelinedomain.KindArticle {
			d.article = &a
		}
		s.deliverables = append(s.deliverables, d)
		return d
	}
	leadD := add(lead)
	leadD.role = RoleLead
	if k, ok := p.Outputs.Derived(); ok {
		d := add(k)
		d.role, d.status, d.sourceID = RoleDerived, StatusWaiting, leadD.id
	}
	return s, nil
}

// Deliverable returns the deliverable with id.
func (s *Story) Deliverable(id uuid.UUID) (*Deliverable, error) {
	for _, d := range s.deliverables {
		if d.id == id {
			return d, nil
		}
	}
	return nil, apperror.New(apperror.NotFound, "deliverable not found")
}

// Deliverables returns the story's deliverables, lead first.
func (s *Story) Deliverables() []*Deliverable { return append([]*Deliverable(nil), s.deliverables...) }

// Lead returns the lead deliverable.
func (s *Story) Lead() *Deliverable {
	for _, d := range s.deliverables {
		if d.role == RoleLead {
			return d
		}
	}
	return nil
}

// Status summarises the story: the least advanced deliverable that still
// needs something, so a story is "review" while anything awaits review.
func (s *Story) Status() Status {
	order := []Status{StatusFailed, StatusReview, StatusDrafting, StatusRendering, StatusQueued, StatusFinalizing,
		StatusPublishing, StatusReady, StatusWaiting, StatusPublished, StatusRejected}
	for _, st := range order {
		for _, d := range s.deliverables {
			if d.status == st {
				return st
			}
		}
	}
	return StatusQueued
}

func (s *Story) touch(now time.Time) { s.updatedAt = timeutil.Normalize(now) }

func (s *Story) ID() uuid.UUID                     { return s.id }
func (s *Story) WorkspaceID() uuid.UUID            { return s.workspaceID }
func (s *Story) PipelineID() uuid.UUID             { return s.pipelineID }
func (s *Story) Origin() Origin                    { return s.origin }
func (s *Story) Language() pipelinedomain.Language { return s.language }
func (s *Story) Idea() Idea                        { return s.idea }
func (s *Story) Brief() Brief                      { return s.brief }
func (s *Story) CreatedBy() uuid.UUID              { return s.createdBy }
func (s *Story) CreatedAt() time.Time              { return s.createdAt }
func (s *Story) UpdatedAt() time.Time              { return s.updatedAt }
func (s *Story) Version() int64                    { return s.version }

// Similarity is the Jaccard index of the two texts' character trigrams
// after fingerprinting, in [0, 1]. Titles that say the same thing in
// slightly different words score around 0.5 and above.
func Similarity(a, b string) float64 {
	ta, tb := trigrams(Fingerprint(a)), trigrams(Fingerprint(b))
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for t := range ta {
		if tb[t] {
			inter++
		}
	}
	return float64(inter) / float64(len(ta)+len(tb)-inter)
}

func trigrams(s string) map[string]bool {
	out := map[string]bool{}
	for w := range strings.FieldsSeq(s) {
		r := []rune(" " + w + " ")
		for i := 0; i+3 <= len(r); i++ {
			out[string(r[i:i+3])] = true
		}
	}
	return out
}

// SearchText is what the duplicate index stores for a story.
func (s *Story) SearchText() string {
	return Fingerprint(s.idea.Title + " " + s.idea.Angle)
}
