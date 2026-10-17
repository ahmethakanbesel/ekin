package handler

import (
	voicehandler "ekin/internal/transport/http/voice/v1"

	"github.com/google/uuid"

	"time"

	"ekin/internal/apperror"
	librarydomain "ekin/internal/domain/library/v1"
	pipelinedomain "ekin/internal/domain/pipeline/v1"
	workspacedomain "ekin/internal/domain/workspace/v1"
	"ekin/internal/gen"
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
	tmpl, err := TemplateChoiceOf(in.TemplateId, in.NoTemplate)
	if err != nil {
		return pipelinedomain.Definition{}, err
	}
	def.TemplateID, def.NoTemplate = tmpl.ID, tmpl.None
	def.Voice = voicehandler.OverrideOf(in.Voice)
	if def.Production, err = ProductionOf(in.Production, in.SelfReviewRounds); err != nil {
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
		if n := in.Video.ShortsPerVideo; n != nil {
			if v, err = v.WithShorts(*n); err != nil {
				return pipelinedomain.Outputs{}, err
			}
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
		def.Outputs.Video = VideoToWire(v)
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
	if p.Template().None {
		none := true
		def.NoTemplate = &none
	}
	def.Voice = voicehandler.OverrideToWire(p.Voice())
	def.SelfReviewRounds = p.Production().SelfReview.Wire()
	def.Production = ProductionToWire(p.Production())
	return def
}

// TemplateChoiceOf parses a wire template choice (shared with ad-hoc
// story requests): a template id, noTemplate, or neither for the
// workspace's default.
func TemplateChoiceOf(id *uuid.UUID, none *bool) (pipelinedomain.TemplateChoice, error) {
	var tid uuid.UUID
	if id != nil {
		tid = *id
	}
	return pipelinedomain.NewTemplateChoice(tid, none != nil && *none)
}

// ProductionOf parses wire production settings and self-review rounds,
// which the wire keeps apart (shared with ad-hoc story requests and the
// workspace defaults); nil takes every default.
func ProductionOf(in *gen.Production, selfReviewRounds *int) (pipelinedomain.Production, error) {
	var out pipelinedomain.Production
	rounds, err := pipelinedomain.ParseSelfReview(selfReviewRounds)
	if err != nil {
		return out, err
	}
	out.SelfReview = rounds
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
	out.Agent, err = pipelinedomain.NewAgent(model, effort)
	if err != nil {
		return out, err
	}
	if in.PreviewQuality != nil {
		if out.PreviewQuality, err = pipelinedomain.ParsePreviewQuality(string(*in.PreviewQuality)); err != nil {
			return out, err
		}
	}
	if in.FinalQuality != nil {
		if out.FinalQuality, err = pipelinedomain.ParseFinalQuality(string(*in.FinalQuality)); err != nil {
			return out, err
		}
	}
	if in.PreviewFps != nil {
		if out.PreviewFPS, err = pipelinedomain.ParsePreviewFPS(int(*in.PreviewFps)); err != nil {
			return out, err
		}
	}
	if in.FinalFps != nil {
		if out.FinalFPS, err = pipelinedomain.ParseFinalFPS(int(*in.FinalFps)); err != nil {
			return out, err
		}
	}
	if in.ReviewAt != nil {
		if out.ReviewAt, err = pipelinedomain.ParseReviewPoint(string(*in.ReviewAt)); err != nil {
			return out, err
		}
	}
	if out.Thumbnails, err = pipelinedomain.ParseThumbnailCount(in.Thumbnails); err != nil {
		return out, err
	}
	if m := in.Media; m != nil {
		for _, kind := range []struct {
			in  *gen.MediaSource
			out *librarydomain.Mode
		}{{m.Music, &out.Media.Music}, {m.Video, &out.Media.Video}, {m.Photo, &out.Media.Photo}} {
			if kind.in == nil {
				continue
			}
			if *kind.out, err = librarydomain.ParseMode(string(*kind.in)); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

// ProductionToWire converts production settings, leaving out what is
// unset; nil when all are. Self-review rounds travel apart (Wire).
func ProductionToWire(p pipelinedomain.Production) *gen.Production {
	p.SelfReview = 0
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
	if q := p.FinalQuality; q != "" {
		quality := gen.ProductionFinalQuality(q)
		out.FinalQuality = &quality
	}
	if n := p.PreviewFPS; n != 0 {
		fps := gen.ProductionPreviewFps(n)
		out.PreviewFps = &fps
	}
	if n := p.FinalFPS; n != 0 {
		fps := gen.ProductionFinalFps(n)
		out.FinalFps = &fps
	}
	if p.ReviewAt != "" {
		at := gen.ProductionReviewAt(p.ReviewAt)
		out.ReviewAt = &at
	}
	if p.Media != (librarydomain.Policy{}) {
		source := func(m librarydomain.Mode) *gen.MediaSource {
			if m == "" {
				return nil
			}
			s := gen.MediaSource(m.String())
			return &s
		}
		out.Media = &gen.MediaSources{Music: source(p.Media.Music), Video: source(p.Media.Video), Photo: source(p.Media.Photo)}
	}
	out.Thumbnails = p.Thumbnails.Wire()
	return out
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
	out := &gen.VideoSpec{
		Format:          gen.VideoSpecFormat(v.Format()),
		Orientation:     gen.VideoSpecOrientation(v.Orientation()),
		DurationSeconds: int(v.Duration() / time.Second),
	}
	if n := v.Shorts(); n > 0 {
		out.ShortsPerVideo = &n
	}
	return out
}

// ArticleToWire converts an article spec.
func ArticleToWire(a pipelinedomain.ArticleSpec) *gen.ArticleSpec {
	tr := make([]gen.Language, 0, len(a.Translations()))
	for _, l := range a.Translations() {
		tr = append(tr, gen.Language(l))
	}
	return &gen.ArticleSpec{Words: a.Words(), Translations: tr}
}
