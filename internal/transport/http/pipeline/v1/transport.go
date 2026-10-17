package handler

import (
	voicehandler "kurgu/internal/transport/http/voice/v1"

	"github.com/google/uuid"

	"time"

	"kurgu/internal/apperror"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	"kurgu/internal/gen"
)

// parseDefinition turns the wire definition into domain values once; the
// service never sees the raw strings.
func parseDefinition(in gen.PipelineDefinition) (pipelinedomain.Definition, error) {
	language, err := pipelinedomain.ParseLanguage(string(in.Language))
	if err != nil {
		return pipelinedomain.Definition{}, err
	}
	outputs, err := ParseOutputs(in.Outputs)
	if err != nil {
		return pipelinedomain.Definition{}, err
	}
	source, err := parseSource(in.Source)
	if err != nil {
		return pipelinedomain.Definition{}, err
	}
	def := pipelinedomain.Definition{Name: in.Name, Language: language, Outputs: outputs, Source: source}
	if in.TemplateId != nil {
		def.TemplateID = *in.TemplateId
	}
	def.Voice = voicehandler.OverrideOf(in.Voice)
	def.SelfReview = SelfReviewOf(in.SelfReviewRounds)
	if def.Production, err = ProductionOf(in.Production); err != nil {
		return pipelinedomain.Definition{}, err
	}
	if in.Schedule != nil {
		loc, err := workspacedomain.ParseTimezone(in.Schedule.Timezone)
		if err != nil {
			return pipelinedomain.Definition{}, err
		}
		s, err := pipelinedomain.ParseSchedule(in.Schedule.Cron, loc, in.Schedule.Batch)
		if err != nil {
			return pipelinedomain.Definition{}, err
		}
		def.Schedule = &s
	}
	return def, nil
}

// ParseOutputs parses wire outputs (shared with ad-hoc story requests).
func ParseOutputs(in gen.Outputs) (pipelinedomain.Outputs, error) {
	var video *pipelinedomain.VideoSpec
	if in.Video != nil {
		v, err := pipelinedomain.NewVideoSpec(
			pipelinedomain.Format(in.Video.Format),
			pipelinedomain.Orientation(in.Video.Orientation),
			time.Duration(in.Video.DurationSeconds)*time.Second)
		if err != nil {
			return pipelinedomain.Outputs{}, err
		}
		video = &v
	}
	var article *pipelinedomain.ArticleSpec
	if in.Article != nil {
		langs := make([]pipelinedomain.Language, 0, len(in.Article.Translations))
		for _, raw := range in.Article.Translations {
			l, err := pipelinedomain.ParseLanguage(string(raw))
			if err != nil {
				return pipelinedomain.Outputs{}, err
			}
			langs = append(langs, l)
		}
		a, err := pipelinedomain.NewArticleSpec(in.Article.Words, langs)
		if err != nil {
			return pipelinedomain.Outputs{}, err
		}
		article = &a
	}
	var lead pipelinedomain.Kind
	if in.Lead != nil {
		k, err := pipelinedomain.ParseKind(string(*in.Lead))
		if err != nil {
			return pipelinedomain.Outputs{}, err
		}
		lead = k
	}
	return pipelinedomain.NewOutputs(video, article, lead)
}

func parseSource(in gen.Source) (pipelinedomain.Source, error) {
	switch pipelinedomain.SourceKind(in.Kind) {
	case pipelinedomain.SourcePrompt:
		if in.Refs != nil && len(*in.Refs) > 0 {
			return pipelinedomain.Source{}, apperror.New(apperror.ValidationFailed, "a prompt source takes no document references")
		}
		return pipelinedomain.NewPromptSource(in.Instructions)
	case pipelinedomain.SourceKnowledge:
		var refs []pipelinedomain.KnowledgeRef
		if in.Refs != nil {
			for _, r := range *in.Refs {
				pages, err := pipelinedomain.NewPageRange(deref(r.FromPage), deref(r.ToPage))
				if err != nil {
					return pipelinedomain.Source{}, err
				}
				ref, err := pipelinedomain.NewKnowledgeRef(r.DocumentId, pages)
				if err != nil {
					return pipelinedomain.Source{}, err
				}
				refs = append(refs, ref)
			}
		}
		return pipelinedomain.NewKnowledgeSource(in.Instructions, refs)
	case pipelinedomain.SourceSEO:
		if in.Refs != nil && len(*in.Refs) > 0 {
			return pipelinedomain.Source{}, apperror.New(apperror.ValidationFailed, "an SEO source takes no document references")
		}
		return pipelinedomain.NewSEOSource(in.Instructions)
	}
	return pipelinedomain.Source{}, apperror.New(apperror.ValidationFailed, "source kind must be prompt, knowledge or seo")
}

func toWire(p *pipelinedomain.Pipeline) gen.Pipeline {
	out := gen.Pipeline{
		Id:          p.ID(),
		WorkspaceId: p.WorkspaceID(),
		Version:     p.Version(),
		Status:      gen.PipelineStatus(p.Status()),
		Definition:  definitionToWire(p),
		NextRunAt:   optTime(p.NextRunAt()),
		LastRunAt:   optTime(p.LastRunAt()),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
	}
	return out
}

func definitionToWire(p *pipelinedomain.Pipeline) gen.PipelineDefinition {
	def := gen.PipelineDefinition{
		Name:     p.Name(),
		Language: gen.Language(p.Language()),
	}
	outputs := p.Outputs()
	lead := gen.DeliverableKind(outputs.Lead())
	def.Outputs.Lead = &lead
	if v, ok := outputs.Video(); ok {
		def.Outputs.Video = &gen.VideoSpec{
			Format:          gen.VideoSpecFormat(v.Format()),
			Orientation:     gen.VideoSpecOrientation(v.Orientation()),
			DurationSeconds: int(v.Duration() / time.Second),
		}
	}
	if a, ok := outputs.Article(); ok {
		tr := make([]gen.Language, 0, len(a.Translations()))
		for _, l := range a.Translations() {
			tr = append(tr, gen.Language(l))
		}
		def.Outputs.Article = &gen.ArticleSpec{Words: a.Words(), Translations: tr}
	}
	source := p.Source()
	def.Source = gen.Source{Kind: gen.SourceKind(source.Kind()), Instructions: source.Instructions()}
	if refs := source.Refs(); len(refs) > 0 {
		wire := make([]gen.KnowledgeRef, 0, len(refs))
		for _, r := range refs {
			ref := gen.KnowledgeRef{DocumentId: r.DocumentID()}
			if !r.Pages().IsWholeDocument() {
				from, to := r.Pages().From(), r.Pages().To()
				ref.FromPage, ref.ToPage = &from, &to
			}
			wire = append(wire, ref)
		}
		def.Source.Refs = &wire
	}
	if s, ok := p.Schedule(); ok {
		def.Schedule = &gen.Schedule{Cron: s.Expr(), Timezone: s.Location().String(), Batch: s.Batch()}
	}
	if tid := p.TemplateID(); tid != uuid.Nil {
		def.TemplateId = &tid
	}
	def.Voice = voicehandler.OverrideToWire(p.Voice())
	rounds := p.SelfReview()
	def.SelfReviewRounds = &rounds
	def.Production = ProductionToWire(p.Production())
	return def
}

// ProductionOf parses wire production settings (shared with ad-hoc
// story requests); nil takes every default.
func ProductionOf(in *gen.Production) (pipelinedomain.Production, error) {
	var out pipelinedomain.Production
	if in == nil {
		return out, nil
	}
	var model, effort string
	if in.DraftModel != nil {
		model = *in.DraftModel
	}
	if in.DraftEffort != nil {
		effort = string(*in.DraftEffort)
	}
	agent, err := pipelinedomain.NewAgent(model, effort)
	if err != nil {
		return out, err
	}
	out.Agent = agent
	if in.PreviewQuality != nil {
		if out.PreviewQuality, err = pipelinedomain.ParsePreviewQuality(string(*in.PreviewQuality)); err != nil {
			return out, err
		}
	}
	if in.ReviewAt != nil {
		if out.ReviewAt, err = pipelinedomain.ParseReviewPoint(string(*in.ReviewAt)); err != nil {
			return out, err
		}
	}
	return out, nil
}

// ProductionToWire converts production settings; nil when all are
// defaults.
func ProductionToWire(p pipelinedomain.Production) *gen.Production {
	if p == (pipelinedomain.Production{}) {
		return nil
	}
	out := &gen.Production{}
	if m := p.Agent.Model; m != "" {
		out.DraftModel = &m
	}
	if e := p.Agent.Effort; e != "" {
		effort := gen.DraftEffort(e)
		out.DraftEffort = &effort
	}
	if q := p.PreviewQuality; q != "" {
		quality := gen.ProductionPreviewQuality(q)
		out.PreviewQuality = &quality
	}
	if p.ReviewAt == pipelinedomain.ReviewScript {
		at := gen.ProductionReviewAtScript
		out.ReviewAt = &at
	}
	return out
}

// SelfReviewOf reads selfReviewRounds, which defaults to one round.
func SelfReviewOf(rounds *int) int {
	if rounds == nil {
		return pipelinedomain.DefaultSelfReview
	}
	return *rounds
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// VideoToWire converts a video spec.
func VideoToWire(v pipelinedomain.VideoSpec) *gen.VideoSpec {
	return &gen.VideoSpec{
		Format:          gen.VideoSpecFormat(v.Format()),
		Orientation:     gen.VideoSpecOrientation(v.Orientation()),
		DurationSeconds: int(v.Duration() / time.Second),
	}
}

// ArticleToWire converts an article spec.
func ArticleToWire(a pipelinedomain.ArticleSpec) *gen.ArticleSpec {
	tr := make([]gen.Language, 0, len(a.Translations()))
	for _, l := range a.Translations() {
		tr = append(tr, gen.Language(l))
	}
	return &gen.ArticleSpec{Words: a.Words(), Translations: tr}
}
