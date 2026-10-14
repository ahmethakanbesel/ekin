package pipeline

import (
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	"kurgu/pkg/timeutil"
)

// Snapshot is the persisted form of a Pipeline. Only repositories build
// one into an aggregate, through Rehydrate.
type Snapshot struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Name        string
	Language    string
	Video       *VideoSnapshot
	Article     *ArticleSnapshot
	Lead        string
	Source      SourceSnapshot
	Schedule    *ScheduleSnapshot
	Status      string
	NextRunAt   time.Time
	LastRunAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int64
}

type VideoSnapshot struct {
	Format          string `json:"format"`
	Orientation     string `json:"orientation"`
	DurationSeconds int    `json:"durationSeconds"`
}

type ArticleSnapshot struct {
	Words        int      `json:"words"`
	Translations []string `json:"translations"`
}

type SourceSnapshot struct {
	Kind         string        `json:"kind"`
	Instructions string        `json:"instructions"`
	Refs         []RefSnapshot `json:"refs,omitempty"`
}

type RefSnapshot struct {
	DocumentID uuid.UUID `json:"documentId"`
	FromPage   int       `json:"fromPage,omitempty"`
	ToPage     int       `json:"toPage,omitempty"`
}

type ScheduleSnapshot struct {
	Cron     string
	Timezone string
	Batch    int
}

// Snapshot returns p's persisted form.
func (p *Pipeline) Snapshot() Snapshot {
	s := Snapshot{
		ID:          p.id,
		WorkspaceID: p.workspaceID,
		Name:        p.def.Name,
		Language:    string(p.def.Language),
		Lead:        string(p.def.Outputs.lead),
		Source:      p.def.Source.snapshot(),
		Status:      string(p.status),
		NextRunAt:   p.nextRunAt,
		LastRunAt:   p.lastRunAt,
		CreatedAt:   p.createdAt,
		UpdatedAt:   p.updatedAt,
		Version:     p.version,
	}
	if v, ok := p.def.Outputs.Video(); ok {
		s.Video = &VideoSnapshot{Format: string(v.format), Orientation: string(v.orientation), DurationSeconds: int(v.duration / time.Second)}
	}
	if a, ok := p.def.Outputs.Article(); ok {
		tr := make([]string, 0, len(a.translations))
		for _, l := range a.translations {
			tr = append(tr, string(l))
		}
		s.Article = &ArticleSnapshot{Words: a.words, Translations: tr}
	}
	if sc := p.def.Schedule; sc != nil {
		s.Schedule = &ScheduleSnapshot{Cron: sc.expr, Timezone: sc.location.String(), Batch: sc.batch}
	}
	return s
}

func (s Source) snapshot() SourceSnapshot {
	out := SourceSnapshot{Kind: string(s.kind), Instructions: s.instructions}
	for _, r := range s.refs {
		out.Refs = append(out.Refs, RefSnapshot{DocumentID: r.documentID, FromPage: r.pages.from, ToPage: r.pages.to})
	}
	return out
}

// Rehydrate rebuilds a Pipeline from storage through the same parsers the
// API uses, so a row that no longer satisfies today's rules surfaces as an
// error instead of an invalid aggregate.
func Rehydrate(s Snapshot) (*Pipeline, error) {
	def, err := DefinitionFromSnapshot(s)
	if err != nil {
		return nil, err
	}
	def, err = checkDefinition(def)
	if err != nil {
		return nil, err
	}
	status := Status(s.Status)
	if status != StatusActive && status != StatusPaused {
		return nil, apperror.New(apperror.InternalError, "stored pipeline status is invalid")
	}
	if s.ID == uuid.Nil || s.WorkspaceID == uuid.Nil {
		return nil, apperror.New(apperror.InternalError, "stored pipeline ids are missing")
	}
	return &Pipeline{
		id:          s.ID,
		workspaceID: s.WorkspaceID,
		def:         def,
		status:      status,
		nextRunAt:   timeutil.Normalize(s.NextRunAt),
		lastRunAt:   timeutil.Normalize(s.LastRunAt),
		createdAt:   timeutil.Normalize(s.CreatedAt),
		updatedAt:   timeutil.Normalize(s.UpdatedAt),
		version:     s.Version,
	}, nil
}

// DefinitionFromSnapshot parses the definition fields of s.
func DefinitionFromSnapshot(s Snapshot) (Definition, error) {
	lang, err := ParseLanguage(s.Language)
	if err != nil {
		return Definition{}, err
	}
	var video *VideoSpec
	if s.Video != nil {
		v, err := NewVideoSpec(Format(s.Video.Format), Orientation(s.Video.Orientation), time.Duration(s.Video.DurationSeconds)*time.Second)
		if err != nil {
			return Definition{}, err
		}
		video = &v
	}
	var article *ArticleSpec
	if s.Article != nil {
		langs := make([]Language, 0, len(s.Article.Translations))
		for _, raw := range s.Article.Translations {
			l, err := ParseLanguage(raw)
			if err != nil {
				return Definition{}, err
			}
			langs = append(langs, l)
		}
		a, err := NewArticleSpec(s.Article.Words, langs)
		if err != nil {
			return Definition{}, err
		}
		article = &a
	}
	outputs, err := NewOutputs(video, article, Kind(s.Lead))
	if err != nil {
		return Definition{}, err
	}
	source, err := sourceFromSnapshot(s.Source)
	if err != nil {
		return Definition{}, err
	}
	var schedule *Schedule
	if s.Schedule != nil {
		loc, err := time.LoadLocation(s.Schedule.Timezone)
		if err != nil {
			return Definition{}, apperror.Wrap(apperror.ValidationFailed, err, "unknown timezone")
		}
		sc, err := ParseSchedule(s.Schedule.Cron, loc, s.Schedule.Batch)
		if err != nil {
			return Definition{}, err
		}
		schedule = &sc
	}
	return Definition{Name: s.Name, Language: lang, Outputs: outputs, Source: source, Schedule: schedule}, nil
}

func sourceFromSnapshot(s SourceSnapshot) (Source, error) {
	switch SourceKind(s.Kind) {
	case SourcePrompt:
		return NewPromptSource(s.Instructions)
	case SourceKnowledge:
		refs := make([]KnowledgeRef, 0, len(s.Refs))
		for _, r := range s.Refs {
			pages, err := NewPageRange(r.FromPage, r.ToPage)
			if err != nil {
				return Source{}, err
			}
			ref, err := NewKnowledgeRef(r.DocumentID, pages)
			if err != nil {
				return Source{}, err
			}
			refs = append(refs, ref)
		}
		return NewKnowledgeSource(s.Instructions, refs)
	case SourceSEO:
		return NewSEOSource(s.Instructions)
	}
	return Source{}, apperror.New(apperror.ValidationFailed, "source kind must be prompt, knowledge or seo")
}

// SnapshotVideo returns v's persisted form.
func SnapshotVideo(v VideoSpec) VideoSnapshot {
	return VideoSnapshot{Format: string(v.format), Orientation: string(v.orientation), DurationSeconds: int(v.duration / time.Second)}
}

// VideoFromSnapshot parses a persisted VideoSpec.
func VideoFromSnapshot(s VideoSnapshot) (VideoSpec, error) {
	return NewVideoSpec(Format(s.Format), Orientation(s.Orientation), time.Duration(s.DurationSeconds)*time.Second)
}

// SnapshotArticle returns a's persisted form.
func SnapshotArticle(a ArticleSpec) ArticleSnapshot {
	langs := make([]string, 0, len(a.translations))
	for _, l := range a.translations {
		langs = append(langs, string(l))
	}
	return ArticleSnapshot{Words: a.words, Translations: langs}
}

// ArticleFromSnapshot parses a persisted ArticleSpec.
func ArticleFromSnapshot(s ArticleSnapshot) (ArticleSpec, error) {
	langs := make([]Language, 0, len(s.Translations))
	for _, raw := range s.Translations {
		l, err := ParseLanguage(raw)
		if err != nil {
			return ArticleSpec{}, err
		}
		langs = append(langs, l)
	}
	return NewArticleSpec(s.Words, langs)
}
