package production

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	seodomain "kurgu/internal/domain/seo/v1"
	storydomain "kurgu/internal/domain/story/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	"kurgu/pkg/id"
)

const (
	// windowTokens is how much source text one idea draws on.
	windowTokens = 6000
	// duplicateScore marks an idea as a repeat without asking a model.
	duplicateScore = 0.6
	// suspectScore asks the Dedupe port (Jev) for a verdict.
	suspectScore = 0.35
	// avoidTitles is how many recent titles the ideator sees.
	avoidTitles = 60
)

// AdHocPayload is the story.adhoc job payload: a structured request plus
// the free-text idea, produced without a pipeline.
type AdHocPayload struct {
	WorkspaceID uuid.UUID                       `json:"workspaceId"`
	ActorID     uuid.UUID                       `json:"actorId"`
	Idea        string                          `json:"idea"`
	Count       int                             `json:"count"`
	Language    string                          `json:"language"`
	Style       string                          `json:"style,omitempty"`
	Video       *pipelinedomain.VideoSnapshot   `json:"video,omitempty"`
	Article     *pipelinedomain.ArticleSnapshot `json:"article,omitempty"`
	Lead        string                          `json:"lead,omitempty"`
	Refs        []storydomain.Ref               `json:"refs,omitempty"`
}

// AdHocRequest is a parsed ad-hoc request.
type AdHocRequest struct {
	Idea     string
	Count    int
	Language pipelinedomain.Language
	Style    string
	Outputs  pipelinedomain.Outputs
	Refs     []storydomain.Ref
}

// MaxAdHocCount bounds one ad-hoc request.
const MaxAdHocCount = 10

// QueueAdHoc validates an ad-hoc request and queues its planning job.
func (s *Service) QueueAdHoc(ctx context.Context, workspaceID, actorID uuid.UUID, req AdHocRequest) (*jobdomain.Job, error) {
	if strings.TrimSpace(req.Idea) == "" {
		return nil, apperror.New(apperror.ValidationFailed, "describe the idea")
	}
	if len([]rune(req.Idea)) > 5000 {
		return nil, apperror.New(apperror.ValidationFailed, "the idea is too long")
	}
	if req.Count < 1 || req.Count > MaxAdHocCount {
		return nil, apperror.New(apperror.ValidationFailed, "count must be between 1 and 10")
	}
	p := AdHocPayload{
		WorkspaceID: workspaceID, ActorID: actorID, Idea: strings.TrimSpace(req.Idea), Count: req.Count,
		Language: string(req.Language), Style: strings.TrimSpace(req.Style), Lead: string(req.Outputs.Lead()), Refs: req.Refs,
	}
	if v, ok := req.Outputs.Video(); ok {
		snap := pipelinedomain.SnapshotVideo(v)
		p.Video = &snap
	}
	if a, ok := req.Outputs.Article(); ok {
		snap := pipelinedomain.SnapshotArticle(a)
		p.Article = &snap
	}
	payload, _ := json.Marshal(p)
	j, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: workspaceID, Queue: jobdomain.QueueControl, Kind: KindAdHoc, Payload: payload, MaxAttempts: 2,
	})
	return j, err
}

// PlanAdHoc runs a story.adhoc job.
func (s *Service) PlanAdHoc(ctx context.Context, p AdHocPayload) error {
	lang, err := pipelinedomain.ParseLanguage(p.Language)
	if err != nil {
		return jobdomain.Permanent(err)
	}
	outputs, err := outputsFromPayload(p)
	if err != nil {
		return jobdomain.Permanent(err)
	}
	instructions := "The person who requested this wrote:\n\n" + p.Idea
	var source string
	if len(p.Refs) > 0 {
		ex, err := s.knowledge.Excerpt(ctx, p.WorkspaceID, toKnowledgeRefs(p.Refs))
		if err != nil {
			return err
		}
		source = ex.Markdown
	}
	ideaPrompt := fmt.Sprintf("Propose %d distinct ideas that fulfil this request.", p.Count)
	if p.Count == 1 {
		ideaPrompt = "Turn this request into exactly one idea, keeping what the person asked for."
	}
	ideas, err := s.ideas(ctx, ideaRequest{
		WorkspaceID: p.WorkspaceID, Language: lang, Instructions: instructions + "\n\n" + ideaPrompt,
		Outputs: outputs, Windows: windowTexts(source), Count: p.Count,
	})
	if err != nil {
		return err
	}
	plans := make([]storydomain.Plan, 0, len(ideas))
	for _, idea := range ideas {
		plans = append(plans, storydomain.Plan{
			WorkspaceID: p.WorkspaceID, Origin: storydomain.OriginAdHoc, Language: lang, Idea: idea.Idea,
			Brief:   storydomain.Brief{Instructions: p.Idea, Style: p.Style, Refs: p.Refs},
			Outputs: outputs, CreatedBy: p.ActorID,
		})
	}
	_, err = s.createStories(ctx, plans, "Ad-hoc request")
	return err
}

func outputsFromPayload(p AdHocPayload) (pipelinedomain.Outputs, error) {
	var (
		video   *pipelinedomain.VideoSpec
		article *pipelinedomain.ArticleSpec
	)
	if p.Video != nil {
		v, err := pipelinedomain.VideoFromSnapshot(*p.Video)
		if err != nil {
			return pipelinedomain.Outputs{}, err
		}
		video = &v
	}
	if p.Article != nil {
		a, err := pipelinedomain.ArticleFromSnapshot(*p.Article)
		if err != nil {
			return pipelinedomain.Outputs{}, err
		}
		article = &a
	}
	return pipelinedomain.NewOutputs(video, article, pipelinedomain.Kind(p.Lead))
}

// PlanRun runs a pipeline.run job: picks Count ideas from the pipeline's
// source, drops repeats of earlier stories, and starts production.
func (s *Service) PlanRun(ctx context.Context, run pipelinesvc.RunPayload) error {
	p, err := s.pipelines.Get(ctx, run.WorkspaceID, run.PipelineID)
	if apperror.Is(err, apperror.PipelineNotFound) {
		return jobdomain.Permanent(err)
	}
	if err != nil {
		return err
	}
	origin := storydomain.OriginManual
	if run.Trigger == "schedule" {
		origin = storydomain.OriginSchedule
	}
	src := p.Source()
	req := ideaRequest{
		WorkspaceID: p.WorkspaceID(), Language: p.Language(), Instructions: src.Instructions(),
		Outputs: p.Outputs(), Count: run.Count,
	}
	var (
		windows [][]storydomain.Ref
		opps    []seodomain.Opportunity
	)
	if src.Kind() == pipelinedomain.SourceSEO {
		if s.seo == nil {
			return jobdomain.Permanent(errors.New("SEO opportunities are not available on this server"))
		}
		if opps, err = s.seo.Next(ctx, p.WorkspaceID(), string(p.Language()), run.Count); err != nil {
			return err
		}
		if len(opps) == 0 {
			return jobdomain.Permanent(errors.New("no open keyword opportunities: refresh them from OpenSEO or import some"))
		}
		for _, o := range opps {
			req.Windows = append(req.Windows, describeOpportunity(o))
		}
		req.Count = len(opps)
	}
	if src.Kind() == pipelinedomain.SourceKnowledge {
		windows, err = s.windows(ctx, p, run.Count)
		if err != nil {
			return err
		}
		for _, w := range windows {
			ex, err := s.knowledge.Excerpt(ctx, p.WorkspaceID(), toKnowledgeRefs(w))
			if err != nil {
				return err
			}
			req.Windows = append(req.Windows, ex.Markdown)
		}
		req.Count = len(windows)
	}
	ideas, err := s.ideas(ctx, req)
	if err != nil {
		return err
	}
	plans := make([]storydomain.Plan, 0, len(ideas))
	for _, idea := range ideas {
		brief := storydomain.Brief{Instructions: src.Instructions()}
		if idea.Window >= 0 && idea.Window < len(windows) {
			brief.Refs = windows[idea.Window]
		}
		if idea.Window >= 0 && idea.Window < len(opps) {
			brief.Keyword, brief.OpportunityID = opps[idea.Window].Keyword, opps[idea.Window].ID
		}
		plans = append(plans, storydomain.Plan{
			WorkspaceID: p.WorkspaceID(), PipelineID: p.ID(), Origin: origin, Language: p.Language(),
			Idea: idea.Idea, Brief: brief, Outputs: p.Outputs(),
		})
	}
	created, err := s.createStories(ctx, plans, p.Name())
	if err != nil {
		return err
	}
	for _, st := range created {
		if b := st.Brief(); b.OpportunityID != uuid.Nil {
			if err := s.seo.MarkUsed(ctx, st.WorkspaceID(), b.OpportunityID, st.ID()); err != nil {
				s.log.WarnContext(ctx, "opportunity not marked used", slog.Any("error", err))
			}
		}
	}
	return nil
}

func describeOpportunity(o seodomain.Opportunity) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Target search keyword: %q", o.Keyword)
	if o.Volume > 0 {
		fmt.Fprintf(&b, " (about %d searches a month", o.Volume)
		if o.Difficulty >= 0 {
			fmt.Fprintf(&b, ", difficulty %d/100", o.Difficulty)
		}
		b.WriteString(")")
	}
	if o.Intent != "" {
		b.WriteString(". Intent: " + o.Intent)
	}
	if o.Note != "" {
		b.WriteString(". " + o.Note)
	}
	b.WriteString(". The idea must answer what someone searching this wants to know.")
	return b.String()
}

func (s *Service) createStories(ctx context.Context, plans []storydomain.Plan, label string) ([]*storydomain.Story, error) {
	if len(plans) == 0 {
		return nil, jobdomain.Permanent(errors.New("no new ideas: every candidate repeated an earlier story"))
	}
	var created []*storydomain.Story
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		for _, plan := range plans {
			st, err := storydomain.New(id.New(), plan, id.New, s.now())
			if err != nil {
				return err
			}
			if err := s.stories.Create(ctx, st); err != nil {
				return err
			}
			if err := s.enqueueStep(ctx, st, st.Lead()); err != nil {
				return err
			}
			created = append(created, st)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, Event{WorkspaceID: plans[0].WorkspaceID, Kind: "run", Title: label,
		Detail: fmt.Sprintf("%d new stories in production", len(plans))})
	return created, nil
}

// windows picks count page windows from the pipeline's knowledge refs,
// continuing after the pages the pipeline's previous story used, so runs
// walk through the material instead of repeating its first pages.
func (s *Service) windows(ctx context.Context, p *pipelinedomain.Pipeline, count int) ([][]storydomain.Ref, error) {
	type page struct {
		doc    uuid.UUID
		number int
		tokens int
	}
	var pages []page
	for _, ref := range p.Source().Refs() {
		costs, err := s.knowledge.PageTokens(ctx, p.WorkspaceID(), ref.DocumentID(), ref.Pages().From(), ref.Pages().To())
		if err != nil {
			return nil, err
		}
		for _, c := range costs {
			if c.Tokens > 0 {
				pages = append(pages, page{doc: ref.DocumentID(), number: c.Number, tokens: c.Tokens})
			}
		}
	}
	if len(pages) == 0 {
		return nil, jobdomain.Permanent(errors.New("the referenced pages have no text"))
	}
	start := 0
	prev, err := s.stories.List(ctx, p.WorkspaceID(), storyrepo.Filter{PipelineID: p.ID(), Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(prev) > 0 {
		if refs := prev[0].Brief().Refs; len(refs) > 0 {
			last := refs[len(refs)-1]
			for i, pg := range pages {
				if pg.doc == last.DocumentID && pg.number == last.To {
					start = (i + 1) % len(pages)
					break
				}
			}
		}
	}
	var out [][]storydomain.Ref
	i := start
	for range count {
		var (
			w      []storydomain.Ref
			tokens int
		)
		for n := 0; n < len(pages) && (tokens < windowTokens || len(w) == 0); n++ {
			pg := pages[i]
			if len(w) > 0 && w[len(w)-1].DocumentID == pg.doc && w[len(w)-1].To == pg.number-1 {
				w[len(w)-1].To = pg.number
			} else {
				w = append(w, storydomain.Ref{DocumentID: pg.doc, From: pg.number, To: pg.number})
			}
			tokens += pg.tokens
			i = (i + 1) % len(pages)
			if i == start && len(out) > 0 {
				break
			}
		}
		out = append(out, w)
	}
	return out, nil
}

func toKnowledgeRefs(refs []storydomain.Ref) []knowledgesvc.Ref {
	out := make([]knowledgesvc.Ref, 0, len(refs))
	for _, r := range refs {
		out = append(out, knowledgesvc.Ref{DocumentID: r.DocumentID, From: r.From, To: r.To})
	}
	return out
}

// windowTexts gives every requested idea the same source text.
func windowTexts(source string) []string {
	if source == "" {
		return nil
	}
	return []string{source}
}

type ideaRequest struct {
	WorkspaceID  uuid.UUID
	Language     pipelinedomain.Language
	Instructions string
	Outputs      pipelinedomain.Outputs
	// Windows are source excerpts; with several, idea i must draw on
	// window i. With one, every idea draws on it.
	Windows []string
	Count   int
}

type plannedIdea struct {
	storydomain.Idea
	Window int
}

var ideasSchema = json.RawMessage(`{
  "type": "object",
  "required": ["ideas"],
  "properties": {
    "ideas": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["title", "angle", "keywords"],
        "properties": {
          "window": {"type": "integer", "description": "index of the source window the idea draws on"},
          "title": {"type": "string", "description": "working title in the content language"},
          "angle": {"type": "string", "description": "2-4 sentences: the specific take, the hook, what the audience learns"},
          "keywords": {"type": "array", "items": {"type": "string"}, "description": "3-6 search keywords people actually type"}
        }
      }
    }
  }
}`)

// ideas asks the agent for ideas and filters out repeats, asking once
// more for replacements when some are dropped.
func (s *Service) ideas(ctx context.Context, req ideaRequest) ([]plannedIdea, error) {
	recent, err := s.stories.List(ctx, req.WorkspaceID, storyrepo.Filter{Limit: avoidTitles})
	if err != nil {
		return nil, err
	}
	avoid := make([]string, 0, len(recent))
	for _, st := range recent {
		avoid = append(avoid, st.Idea().Title)
	}
	var kept []plannedIdea
	want := req.Count
	for round := 0; round < 2 && len(kept) < req.Count; round++ {
		candidates, err := s.askIdeas(ctx, req, want, avoid)
		if err != nil {
			return nil, err
		}
		for _, c := range candidates {
			if len(kept) == req.Count {
				break
			}
			dup, why, err := s.isDuplicate(ctx, req.WorkspaceID, c.Idea, kept)
			if err != nil {
				return nil, err
			}
			avoid = append(avoid, c.Title)
			if dup {
				s.log.InfoContext(ctx, "idea dropped as a repeat", slog.String("title", c.Title), slog.String("reason", why))
				continue
			}
			kept = append(kept, c)
		}
		want = req.Count - len(kept)
	}
	return kept, nil
}

func (s *Service) isDuplicate(ctx context.Context, workspaceID uuid.UUID, idea storydomain.Idea, batch []plannedIdea) (bool, string, error) {
	for _, b := range batch {
		if storydomain.Similarity(idea.Title, b.Title) >= duplicateScore {
			return true, "repeats " + b.Title + " in this batch", nil
		}
	}
	matches, err := s.stories.Similar(ctx, workspaceID, idea.Title+" "+idea.Angle, 5)
	if err != nil {
		return false, "", err
	}
	// Rank by title against title: the angle widens the search, but
	// two different takes on a topic share many words.
	for i := range matches {
		matches[i].Score = storydomain.Similarity(idea.Title, matches[i].Title)
	}
	slices.SortStableFunc(matches, func(a, b storyrepo.Match) int { return cmp.Compare(b.Score, a.Score) })
	if len(matches) == 0 || matches[0].Score < suspectScore {
		return false, "", nil
	}
	if matches[0].Score >= duplicateScore {
		return true, "repeats " + matches[0].Title, nil
	}
	if s.dedupe == nil {
		return false, "", nil
	}
	return s.dedupe.Duplicate(ctx, idea, matches)
}

func (s *Service) askIdeas(ctx context.Context, req ideaRequest, count int, avoid []string) ([]plannedIdea, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "You plan content for a %s-language channel and blog. Produce %d ideas as JSON.\n\n", languageName(req.Language), count)
	fmt.Fprintf(&b, "Each idea becomes: %s.\n\n", describeOutputs(req.Outputs))
	if req.Instructions != "" {
		b.WriteString("## Brief\n\n" + req.Instructions + "\n\n")
	}
	switch {
	case len(req.Windows) > 1:
		fmt.Fprintf(&b, "## Source windows\n\nIdea i must draw only on window i (set \"window\": i, from 0). Every claim must be supported by its window.\n\n")
		for i, w := range req.Windows {
			fmt.Fprintf(&b, "### Window %d\n\n%s\n\n", i, w)
		}
	case len(req.Windows) == 1:
		b.WriteString("## Source\n\nBase every idea on this material (set \"window\": 0):\n\n" + req.Windows[0] + "\n\n")
	}
	if len(avoid) > 0 {
		b.WriteString("## Already covered — do not repeat these topics or near-variants\n\n")
		for _, t := range avoid {
			b.WriteString("- " + t + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Titles are specific and concrete, written in the content language, no clickbait, no emojis. ")
	b.WriteString("Ideas must differ from each other in substance, not just wording.")

	res, err := s.agent.Run(ctx, claude.Request{Dir: s.cfg.WorkRoot, Prompt: b.String(), JSONSchema: ideasSchema, Timeout: 10 * time.Minute})
	if err != nil {
		return nil, fmt.Errorf("ideas: %w", err)
	}
	var out struct {
		Ideas []struct {
			Window   *int     `json:"window"`
			Title    string   `json:"title"`
			Angle    string   `json:"angle"`
			Keywords []string `json:"keywords"`
		} `json:"ideas"`
	}
	if err := json.Unmarshal(res.Structured, &out); err != nil {
		return nil, fmt.Errorf("ideas: decode: %w", err)
	}
	var ideas []plannedIdea
	for i, raw := range out.Ideas {
		idea, err := storydomain.NewIdea(raw.Title, raw.Angle, raw.Keywords)
		if err != nil {
			continue
		}
		w := -1
		switch {
		case raw.Window != nil:
			w = *raw.Window
		case len(req.Windows) > 1:
			w = i
		case len(req.Windows) == 1:
			w = 0
		}
		ideas = append(ideas, plannedIdea{Idea: idea, Window: w})
	}
	return ideas, nil
}

func languageName(l pipelinedomain.Language) string {
	if l == pipelinedomain.English {
		return "English"
	}
	return "Turkish"
}

func describeOutputs(o pipelinedomain.Outputs) string {
	var parts []string
	if v, ok := o.Video(); ok {
		parts = append(parts, fmt.Sprintf("a %s %s video of about %d seconds", v.Format(), v.Orientation(), int(v.Duration().Seconds())))
	}
	if a, ok := o.Article(); ok {
		parts = append(parts, fmt.Sprintf("a blog article of about %d words", a.Words()))
	}
	return strings.Join(parts, " and ")
}

func isStale(err error) bool { return apperror.Is(err, apperror.StaleVersion) }
