package production_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/adapter/extract"
	"kurgu/internal/adapter/render"
	"kurgu/internal/blob"
	"kurgu/internal/db/dbtest"
	jobdomain "kurgu/internal/domain/job/v1"
	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	storydomain "kurgu/internal/domain/story/v1"
	templatedomain "kurgu/internal/domain/template/v1"
	workspacedomain "kurgu/internal/domain/workspace/v1"
	jobrepo "kurgu/internal/repository/job/v1"
	knowledgerepo "kurgu/internal/repository/knowledge/v1"
	pipelinerepo "kurgu/internal/repository/pipeline/v1"
	seorepo "kurgu/internal/repository/seo/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	storyrepo "kurgu/internal/repository/story/v1"
	templaterepo "kurgu/internal/repository/template/v1"
	workspacerepo "kurgu/internal/repository/workspace/v1"
	jobsvc "kurgu/internal/service/job/v1"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	production "kurgu/internal/service/production/v1"
	seosvc "kurgu/internal/service/seo/v1"
	templatesvc "kurgu/internal/service/template/v1"
	"kurgu/pkg/id"
)

// fakeAgent plays the authoring agent: it answers idea prompts with
// structured JSON and "writes" deliverables into the working directory.
type fakeAgent struct {
	mu      sync.Mutex
	prompts []claude.Request
	titles  []string
}

func (a *fakeAgent) Run(_ context.Context, req claude.Request) (claude.Result, error) {
	a.mu.Lock()
	a.prompts = append(a.prompts, req)
	a.mu.Unlock()
	if len(req.JSONSchema) > 0 {
		var ideas []map[string]any
		for _, t := range a.titles {
			ideas = append(ideas, map[string]any{"title": t, "angle": "Bedensel belirtiler üzerinden anlatım.", "keywords": []string{"kaygı"}})
		}
		b, _ := json.Marshal(map[string]any{"ideas": ideas})
		return claude.Result{SessionID: "ideas", Structured: b}, nil
	}
	write := func(name, content string) {
		p := filepath.Join(req.Dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		_ = os.WriteFile(p, []byte(content), 0o600)
	}
	instructions, _ := os.ReadFile(filepath.Join(req.Dir, "INSTRUCTIONS.md"))
	switch {
	case strings.Contains(string(instructions), "produce a"):
		write("script.md", "Sahne 1: Kaygı nedir?")
		write("narration/narration.txt", "Kaygı nedir?")
		write("narration/timings.json", `{"duration": 20, "words": [{"text":"Kaygı","start":0,"end":0.5}]}`)
		write("narration/speech.wav", "RIFF")
		write("composition.html", `<script src="/kit/kurgu.js"></script>`)
		write("metadata.json", `{"title":"Kaygı nedir?","description":"Kısa açıklama.","tags":["kaygı"]}`)
	default:
		body := strings.Repeat("Kaygı ile ilgili açıklayıcı bir cümle. ", 150)
		lead, _ := os.ReadFile(filepath.Join(req.Dir, "sources", "lead.md"))
		if len(lead) > 0 {
			body = "Videoya dayanan yazı. " + body
		}
		write("article.md", "---\ntitle: \"Kaygı nedir\"\ndescription: \"Kaygının bedensel ve zihinsel belirtilerini, endişeden farkını ve ne zaman destek almak gerektiğini anlatan yazı.\"\nslug: kaygi-nedir\ntags: [kaygi]\n---\n\n"+body)
	}
	return claude.Result{SessionID: "session-" + filepath.Base(req.Dir), Text: "done"}, nil
}

// fakeRenderer writes placeholder files instead of driving Chrome.
type fakeRenderer struct{}

func (fakeRenderer) Probe(context.Context, string, string) (render.Meta, []string, error) {
	return render.Meta{Width: 1080, Height: 1920, FPS: 30, Duration: 21}, nil, nil
}
func (fakeRenderer) Video(_ context.Context, _, _ string, _ render.VideoOptions, out string) (render.Meta, error) {
	return render.Meta{Width: 1080, Height: 1920, FPS: 30, Duration: 21}, os.WriteFile(out, []byte("video"), 0o600)
}
func (fakeRenderer) Music(context.Context, string, float64, string) (float64, error) { return 0.5, nil }
func (fakeRenderer) Mix(_ context.Context, _, _, _ string, _ float64, out string) error {
	return os.WriteFile(out, []byte("mixed"), 0o600)
}
func (fakeRenderer) Poster(_ context.Context, _ string, _ float64, out string) error {
	return os.WriteFile(out, []byte("jpg"), 0o600)
}

type fakePublisher struct{ kind pipelinedomain.Kind }

func (p fakePublisher) Kind() pipelinedomain.Kind                      { return p.kind }
func (p fakePublisher) Ready(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (p fakePublisher) Publish(ctx context.Context, req production.PublishRequest) (storydomain.Publication, error) {
	name := storydomain.ArtifactFinal
	if p.kind == pipelinedomain.KindArticle {
		name = storydomain.ArtifactArticle
	}
	rc, _, err := req.Open(ctx, name)
	if err != nil {
		return storydomain.Publication{}, err
	}
	_, _ = io.ReadAll(rc)
	_ = rc.Close()
	return storydomain.Publication{URL: "https://example.com/" + string(p.kind), ExternalID: "ext-" + string(p.kind)}, nil
}

type fixture struct {
	seo       *seosvc.Service
	svc       *production.Service
	jobs      *jobsvc.Service
	pipelines *pipelinesvc.Service
	agent     *fakeAgent
	ws        uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	_, tx := dbtest.Open(t)
	w, _ := workspacedomain.New(id.New(), "acme", time.UTC, time.Now())
	workspaces := workspacerepo.NewSQLite(tx)
	if err := workspaces.Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	blobs, _ := blob.NewFS(t.TempDir())
	jobs := jobsvc.NewService(jobrepo.NewSQLite(tx), tx)
	knowledge := knowledgesvc.NewService(knowledgerepo.NewSQLite(tx), blobs, jobs, extract.New(), tx)
	pipelines := pipelinesvc.NewService(pipelinerepo.NewSQLite(tx), workspaces, jobs, tx).WithDocuments(knowledge)
	agent := &fakeAgent{titles: []string{"Kaygı ile endişe arasındaki fark"}}
	seo := seosvc.NewService(seorepo.NewSQLite(tx), settingsrepo.NewSQLite(tx), jobs, agent, t.TempDir())
	templates := templatesvc.NewService(templaterepo.NewSQLite(tx), jobs, agent, t.TempDir())
	pipelines.WithTemplates(templates)
	svc := production.NewService(production.Config{WorkRoot: t.TempDir(), KurguBin: "/usr/local/bin/kurgu"}, production.Deps{
		Stories: storyrepo.NewSQLite(tx), Pipelines: pipelines, Knowledge: knowledge, Jobs: jobs, Tx: tx,
		Blobs: blobs, Agent: agent, Renderer: fakeRenderer{}, SEO: seo, Templates: templates,
	})
	svc.AddPublisher(fakePublisher{pipelinedomain.KindVideo}).AddPublisher(fakePublisher{pipelinedomain.KindArticle})
	return fixture{seo: seo, svc: svc, jobs: jobs, pipelines: pipelines, agent: agent, ws: w.ID()}
}

// drain runs queued jobs until none are left, like a worker would.
func (f fixture) drain(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	kinds := []string{pipelinesvc.KindRun, production.KindAdHoc, production.KindDraft, production.KindRender, production.KindPublish}
	for range 50 {
		j, err := f.jobs.Claim(ctx, jobdomain.Queues, kinds, "test", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			return
		}
		var runErr error
		switch j.Kind() {
		case pipelinesvc.KindRun:
			var p pipelinesvc.RunPayload
			_ = json.Unmarshal(j.Payload(), &p)
			runErr = f.svc.PlanRun(ctx, p)
		case production.KindAdHoc:
			var p production.AdHocPayload
			_ = json.Unmarshal(j.Payload(), &p)
			runErr = f.svc.PlanAdHoc(ctx, p)
		default:
			var p production.DeliverablePayload
			_ = json.Unmarshal(j.Payload(), &p)
			switch j.Kind() {
			case production.KindDraft:
				runErr = f.svc.Draft(ctx, p)
			case production.KindRender:
				runErr = f.svc.Render(ctx, p)
			case production.KindPublish:
				runErr = f.svc.PublishJob(ctx, p)
			}
		}
		if runErr != nil {
			t.Fatalf("%s: %v", j.Kind(), runErr)
		}
		_ = f.jobs.Complete(ctx, j.ID(), "test", nil)
	}
	t.Fatal("jobs never drained")
}

func (f fixture) story(t *testing.T) *storydomain.Story {
	t.Helper()
	list, err := f.svc.List(context.Background(), f.ws, production.Filter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("stories = %d, %v", len(list), err)
	}
	return list[0]
}

func TestVideoWithDerivedArticleAndTranslation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	article, _ := pipelinedomain.NewArticleSpec(900, []pipelinedomain.Language{pipelinedomain.English})
	outputs, _ := pipelinedomain.NewOutputs(&video, &article, pipelinedomain.KindVideo)
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine eğitici içerik.")
	p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "Shorts", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pipelines.RunNow(ctx, f.ws, p.ID(), 1); err != nil {
		t.Fatal(err)
	}
	f.drain(t)

	st := f.story(t)
	lead := st.Lead()
	if st.Idea().Title != "Kaygı ile endişe arasındaki fark" || lead.Status() != storydomain.StatusReview || lead.Revision() != 1 {
		t.Fatalf("after run: %q %s r%d", st.Idea().Title, lead.Status(), lead.Revision())
	}
	revs, _ := f.svc.Revisions(ctx, f.ws, st.ID(), lead.ID())
	for _, name := range []string{storydomain.ArtifactWorkdir, storydomain.ArtifactPreview, storydomain.ArtifactPoster, storydomain.ArtifactMetadata} {
		if revs[0].Artifacts[name] == "" {
			t.Fatalf("revision 1 lacks %s: %v", name, revs[0].Artifacts)
		}
	}
	brief := f.agent.prompts[1]
	if !strings.Contains(strings.Join(brief.Allowed, ","), "Bash(kurgu tool:*)") || !strings.Contains(strings.Join(brief.Env, " "), "KURGU_TOOL_SPEC") ||
		!strings.Contains(strings.Join(brief.Env, " "), ".bin") {
		t.Fatalf("agent request = %+v", brief)
	}

	// Changes resume the agent session with the feedback.
	at := 4.0
	fb, _ := storydomain.NewFeedback("Kancayı hızlandır", storydomain.ScopeScript, &at, id.New(), time.Now())
	if _, err := f.svc.RequestChanges(ctx, f.ws, st.ID(), lead.ID(), fb); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	last := f.agent.prompts[len(f.agent.prompts)-1]
	if last.Resume == "" || !strings.Contains(last.Prompt, "Kancayı hızlandır") || !strings.Contains(last.Prompt, "4.0 s") {
		t.Fatalf("revision prompt = %+v", last)
	}
	st = f.story(t)
	if st.Lead().Revision() != 2 || st.Lead().Status() != storydomain.StatusReview {
		t.Fatalf("after changes: r%d %s", st.Lead().Revision(), st.Lead().Status())
	}

	// Approval: final render, publish, then the derived article.
	if _, err := f.svc.Approve(ctx, f.ws, st.ID(), st.Lead().ID(), id.New()); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	st = f.story(t)
	lead = st.Lead()
	articleD := st.Deliverables()[1]
	if lead.Status() != storydomain.StatusPublished || lead.Publication().URL != "https://example.com/video" {
		t.Fatalf("lead = %s %+v", lead.Status(), lead.Publication())
	}
	if articleD.Status() != storydomain.StatusReview || articleD.Kind() != pipelinedomain.KindArticle {
		t.Fatalf("derived = %s: %s", articleD.Status(), articleD.Failure())
	}
	brief = f.agent.prompts[len(f.agent.prompts)-1]
	instructions, _ := os.ReadFile(filepath.Join(brief.Dir, "INSTRUCTIONS.md"))
	if !strings.Contains(string(instructions), `<YouTube id="ext-video" />`) {
		t.Fatalf("derived article does not embed the published video:\n%s", instructions)
	}

	if _, err := f.svc.Approve(ctx, f.ws, st.ID(), articleD.ID(), id.New()); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	st = f.story(t)
	ds := st.Deliverables()
	if len(ds) != 3 || ds[1].Status() != storydomain.StatusPublished || ds[2].Role() != storydomain.RoleTranslation || ds[2].Status() != storydomain.StatusReview {
		t.Fatalf("after article approval: %d deliverables, %s / %s", len(ds), ds[1].Status(), ds[len(ds)-1].Status())
	}
	if _, err := os.Stat(filepath.Join(f.agent.prompts[len(f.agent.prompts)-1].Dir, "source.md")); err != nil {
		t.Fatal("translation did not get the source article")
	}
}

func TestDuplicateIdeasAreDropped(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	req := production.AdHocRequest{Idea: "Kaygı ve endişe farkı", Count: 1, Language: pipelinedomain.Turkish, Outputs: outputs}
	if _, err := f.svc.QueueAdHoc(ctx, f.ws, id.New(), req); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	st := f.story(t)
	if st.Origin() != storydomain.OriginAdHoc || st.Brief().Instructions != "Kaygı ve endişe farkı" {
		t.Fatalf("ad-hoc story = %+v", st.Snapshot())
	}

	// The agent proposes the same idea again: no second story, and the
	// planning job fails permanently with a clear reason.
	if _, err := f.svc.QueueAdHoc(ctx, f.ws, id.New(), req); err != nil {
		t.Fatal(err)
	}
	j, _ := f.jobs.Claim(ctx, []jobdomain.Queue{jobdomain.QueueControl}, []string{production.KindAdHoc}, "t", time.Minute)
	var p production.AdHocPayload
	_ = json.Unmarshal(j.Payload(), &p)
	err := f.svc.PlanAdHoc(ctx, p)
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("duplicate plan err = %v", err)
	}
	ideaPrompt := f.agent.prompts[len(f.agent.prompts)-1].Prompt
	if !strings.Contains(ideaPrompt, "Kaygı ile endişe arasındaki fark") {
		t.Fatal("the ideator was not told what is already covered")
	}
}

func TestParseArticle(t *testing.T) {
	body := strings.Repeat("kelime ", 150)
	good := "---\ntitle: Başlık\ndescription: \"" + strings.Repeat("a", 120) + "\"\nslug: baslik-iki\ntags: [a]\n---\n\n" + body
	a, err := production.ParseArticle([]byte(good))
	if err != nil || a.Words != 150 || a.Slug != "baslik-iki" {
		t.Fatalf("parse = %+v, %v", a, err)
	}
	for name, bad := range map[string]string{
		"no frontmatter": body,
		"bad slug":       strings.Replace(good, "baslik-iki", "Başlık İki", 1),
		"short desc":     strings.Replace(good, strings.Repeat("a", 120), "kısa", 1),
		"h1 body":        strings.Replace(good, "\n\n"+body, "\n\n# H1\n"+body, 1),
	} {
		if _, err := production.ParseArticle([]byte(bad)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSEOSourceTargetsBestOpportunity(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	n, err := f.seo.Import(ctx, f.ws, []seosvc.Item{
		{Keyword: "ruminasyon nedir", Language: "tr", Volume: 2400, Difficulty: 18, Intent: "informational"},
		{Keyword: "kaygı testi", Language: "tr", Volume: 900, Difficulty: 70},
		{Keyword: "Ruminasyon  NEDİR", Language: "tr"},
	}, "manual")
	if err != nil || n != 2 {
		t.Fatalf("import = %d %v", n, err)
	}
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	source, _ := pipelinedomain.NewSEOSource("Arama yapan yetişkinlere eğitici kısa videolar.")
	p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "SEO", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	f.drain(t)
	st := f.story(t)
	if st.Brief().Keyword != "ruminasyon nedir" {
		t.Fatalf("keyword = %q", st.Brief().Keyword)
	}
	brief, _ := os.ReadFile(filepath.Join(f.agent.prompts[len(f.agent.prompts)-1].Dir, "BRIEF.md"))
	if !strings.Contains(string(brief), `Target search keyword:** "ruminasyon nedir"`) {
		t.Fatalf("brief lacks the keyword:\n%s", brief)
	}
	open, _ := f.seo.Next(ctx, f.ws, "tr", 5)
	if len(open) != 1 || open[0].Keyword != "kaygı testi" {
		t.Fatalf("open after run = %+v", open)
	}
}

func TestTemplateReachesTheAgent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
	def := pipelinedomain.Definition{Name: "S", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source,
		TemplateID: templatedomain.BuiltinID("evidence-guide")}
	if _, err := f.pipelines.Create(ctx, f.ws, def); err == nil {
		t.Fatal("an article-only template was accepted for a video pipeline")
	}
	def.TemplateID = templatedomain.BuiltinID("cosmic-flat")
	p, err := f.pipelines.Create(ctx, f.ws, def)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	f.drain(t)
	st := f.story(t)
	if st.Brief().Template == nil || st.Brief().Template.Name != "Cosmic flat explainer" {
		t.Fatalf("story template = %+v", st.Brief().Template)
	}
	dir := f.agent.prompts[len(f.agent.prompts)-1].Dir
	style, err := os.ReadFile(filepath.Join(dir, "STYLE.md"))
	if err != nil || !strings.Contains(string(style), "#0B1D3A") || !strings.Contains(string(style), "never use any channel's name") {
		t.Fatalf("STYLE.md = %s %v", style, err)
	}
	brief, _ := os.ReadFile(filepath.Join(dir, "BRIEF.md"))
	if !strings.Contains(string(brief), "Follow `STYLE.md`") {
		t.Fatal("brief does not point at STYLE.md")
	}
}
