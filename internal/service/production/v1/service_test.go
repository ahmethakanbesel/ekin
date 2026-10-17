package production_test

import (
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"kurgu/internal/apperror"
	"os"
	"path/filepath"
	"slices"
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
	seodomain "kurgu/internal/domain/seo/v1"
	storydomain "kurgu/internal/domain/story/v1"
	templatedomain "kurgu/internal/domain/template/v1"
	voicedomain "kurgu/internal/domain/voice/v1"
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
	voicesvc "kurgu/internal/service/voice/v1"
	"kurgu/pkg/id"
)

// fakeAgent plays the authoring agent: it answers idea prompts with
// structured JSON and "writes" deliverables into the working directory.
type fakeAgent struct {
	mu      sync.Mutex
	prompts []claude.Request
	titles  []string
	// seen holds article.md or narration/narration.txt as each draft
	// found them, before writing.
	seen []string
	// stop, when set, is called by the next draft, which then fails as
	// cut short: the worker stopping mid-draft.
	stop func()
	// verdicts are the editor's answers to self-review requests, in
	// order; when they run out the editor passes. reviewErr fails them.
	verdicts  []string
	reviewErr error
	reviews   []claude.Request
	// onIdeas, when set, runs once while the agent writes ideas: what
	// happens elsewhere during a planning job.
	onIdeas func()
}

func (a *fakeAgent) Run(ctx context.Context, req claude.Request) (claude.Result, error) {
	a.mu.Lock()
	a.prompts = append(a.prompts, req)
	stop := a.stop
	if len(req.JSONSchema) == 0 {
		a.stop = nil
	}
	a.mu.Unlock()
	if stop != nil && len(req.JSONSchema) == 0 {
		stop()
		return claude.Result{}, ctx.Err()
	}
	if strings.Contains(string(req.JSONSchema), `"verdict"`) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.reviews = append(a.reviews, req)
		if a.reviewErr != nil {
			return claude.Result{}, a.reviewErr
		}
		answer := `{"verdict":"pass","issues":[]}`
		if len(a.verdicts) > 0 {
			answer, a.verdicts = a.verdicts[0], a.verdicts[1:]
		}
		return claude.Result{SessionID: "editor", Structured: json.RawMessage(answer)}, nil
	}
	if len(req.JSONSchema) > 0 {
		a.mu.Lock()
		onIdeas := a.onIdeas
		a.onIdeas = nil
		a.mu.Unlock()
		if onIdeas != nil {
			onIdeas()
		}
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
	seen, err := os.ReadFile(filepath.Join(req.Dir, "article.md"))
	if err != nil {
		seen, _ = os.ReadFile(filepath.Join(req.Dir, "narration", "narration.txt"))
	}
	a.mu.Lock()
	a.seen = append(a.seen, string(seen))
	a.mu.Unlock()
	switch {
	case strings.Contains(string(instructions), "produce a"):
		write("script.md", "Sahne 1: Kaygı nedir?")
		write("narration/narration.txt", "Kaygı nedir?")
		write("narration/timings.json", `{"engine": "ema", "voice": "ema", "duration": 20, "words": [{"text":"Kaygı","start":0,"end":0.5}]}`)
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
// fakeRenderer writes stand-in files; with scales set it records the
// scale of every video it renders.
type fakeRenderer struct{ scales *scaleLog }

type scaleLog struct {
	mu     sync.Mutex
	scales []float64
}

func (l *scaleLog) all() []float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]float64(nil), l.scales...)
}

func (fakeRenderer) Probe(context.Context, string, string) (render.Meta, []string, error) {
	return render.Meta{Width: 1080, Height: 1920, FPS: 30, Duration: 21}, nil, nil
}
func (r fakeRenderer) Video(_ context.Context, _, _ string, opts render.VideoOptions, out string) (render.Meta, error) {
	if r.scales != nil {
		r.scales.mu.Lock()
		r.scales.scales = append(r.scales.scales, opts.Scale)
		r.scales.mu.Unlock()
	}
	return render.Meta{Width: 1080, Height: 1920, FPS: 30, Duration: 21}, os.WriteFile(out, []byte("video"), 0o600)
}
func (fakeRenderer) Music(context.Context, string, float64, string) (float64, error) { return 0.5, nil }
func (fakeRenderer) Mix(_ context.Context, _, _, _ string, duration float64, out string) error {
	return os.WriteFile(out, tinyMP4(duration), 0o600)
}

// tinyMP4 is just enough of an MP4 to carry a length: ftyp, then a moov
// holding a version 0 mvhd with a millisecond timescale.
func tinyMP4(seconds float64) []byte {
	box := func(kind string, body []byte) []byte {
		return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), kind...), body...)
	}
	mvhd := binary.BigEndian.AppendUint32(make([]byte, 12), 1000)
	mvhd = binary.BigEndian.AppendUint32(mvhd, uint32(seconds*1000))
	return append(box("ftyp", []byte("isom\x00\x00\x02\x00")), box("moov", box("mvhd", append(mvhd, make([]byte, 80)...)))...)
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
	voices    *voicesvc.Service
	seo       *seosvc.Service
	svc       *production.Service
	jobs      *jobsvc.Service
	pipelines *pipelinesvc.Service
	agent     *fakeAgent
	ws        uuid.UUID
	// host returns another production service on the same database and
	// blobs with its own work root, like a second worker host.
	host func() *production.Service
	// hostWith is host with some of its dependencies swapped.
	hostWith func(adjust func(*production.Deps)) *production.Service
	blobs    blob.Store
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
	voices := voicesvc.NewService(settingsrepo.NewSQLite(tx), jobs, nil, blobs)
	pipelines.WithTemplates(templates).WithVoices(voices)
	hostWith := func(adjust func(*production.Deps)) *production.Service {
		deps := production.Deps{
			Stories: storyrepo.NewSQLite(tx), Pipelines: pipelines, Knowledge: knowledge, Jobs: jobs, Tx: tx,
			Blobs: blobs, Agent: agent, Renderer: fakeRenderer{}, SEO: seo, Templates: templates, Voices: voices,
		}
		if adjust != nil {
			adjust(&deps)
		}
		svc := production.NewService(production.Config{WorkRoot: t.TempDir(), KurguBin: "/usr/local/bin/kurgu"}, deps)
		svc.AddPublisher(fakePublisher{pipelinedomain.KindVideo}).AddPublisher(fakePublisher{pipelinedomain.KindArticle})
		return svc
	}
	host := func() *production.Service { return hostWith(nil) }
	svc := host()
	// As transport/job/production.Watch does in the app.
	for _, kind := range []string{production.KindDraft, production.KindRender, production.KindPublish} {
		jobs.OnEnded(kind, func(ctx context.Context, j *jobdomain.Job) error {
			var p production.DeliverablePayload
			_ = json.Unmarshal(j.Payload(), &p)
			return svc.Abandoned(ctx, j.Kind(), j.ID(), p, j.LastError())
		})
	}
	return fixture{voices: voices, seo: seo, svc: svc, jobs: jobs, pipelines: pipelines, agent: agent, ws: w.ID(), host: host,
		blobs: blobs, hostWith: hostWith}
}

// drain runs queued jobs until none are left, like a worker would.
func (f fixture) drain(t *testing.T) {
	t.Helper()
	f.drainOn(t, f.svc)
}

// drainOn runs queued jobs on the given host.
func (f fixture) drainOn(t *testing.T, svc *production.Service) {
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
		ctx := jobdomain.WithTrace(ctx, j.TraceID()) // as the runner does
		var runErr error
		switch j.Kind() {
		case pipelinesvc.KindRun:
			var p pipelinesvc.RunPayload
			_ = json.Unmarshal(j.Payload(), &p)
			runErr = svc.PlanRun(ctx, p)
		case production.KindAdHoc:
			var p production.AdHocPayload
			_ = json.Unmarshal(j.Payload(), &p)
			runErr = svc.PlanAdHoc(ctx, p)
		default:
			var p production.DeliverablePayload
			_ = json.Unmarshal(j.Payload(), &p)
			switch j.Kind() {
			case production.KindDraft:
				runErr = svc.Draft(ctx, p)
			case production.KindRender:
				runErr = svc.Render(ctx, p)
			case production.KindPublish:
				runErr = svc.PublishJob(ctx, j.ID(), p)
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
	list, _, err := f.svc.List(context.Background(), f.ws, production.Filter{})
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
	// The narration's words and voice, and the rendered preview's length.
	ema := storydomain.Narrator{Engine: "ema", Voice: "ema"}
	if want := (storydomain.Measures{Duration: 21 * time.Second, Words: 2, Narrator: ema}); revs[0].Measures != want || lead.Measures() != want {
		t.Fatalf("measures = %+v / %+v, want %+v", revs[0].Measures, lead.Measures(), want)
	}
	brief := f.agent.prompts[1]
	if !strings.Contains(strings.Join(brief.Allowed, ","), "Bash(kurgu tool:*)") || !strings.Contains(strings.Join(brief.Env, " "), "KURGU_TOOL_SPEC") ||
		!strings.Contains(strings.Join(brief.Env, " "), ".bin") {
		t.Fatalf("agent request = %+v", brief)
	}
	// A Turkish short is scripted for a brisk pace and the Shorts layout.
	instructions, _ := os.ReadFile(filepath.Join(brief.Dir, "INSTRUCTIONS.md"))
	for _, want := range []string{"about\n   2.0 words per second", "**Layout:**", "**Rhythm:**", "`WRITING.md`", "read-aloud pass"} {
		if !strings.Contains(string(instructions), want) {
			t.Fatalf("video instructions lack %q:\n%s", want, instructions)
		}
	}
	// It is written in Turkish from the start, following the writing guide.
	if guide, err := os.ReadFile(filepath.Join(brief.Dir, "WRITING.md")); err != nil || !strings.Contains(string(guide), "Çeviri kokan kalıplar") {
		t.Fatalf("WRITING.md = %.80s %v", guide, err)
	}

	// Changes resume the agent session with every note of the round.
	at, later := 4.0, 31.5
	fb, _ := storydomain.NewFeedback([]storydomain.Note{
		{Text: "Grafik yanlış", Scope: storydomain.ScopeVisuals, AtSeconds: &later},
		{Text: "Kancayı hızlandır", Scope: storydomain.ScopeScript, AtSeconds: &at},
		{Text: "Daha sakin bir ton"},
	}, id.New(), time.Now())
	if _, err := f.svc.RequestChanges(ctx, f.ws, st.ID(), lead.ID(), fb); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	last := f.agent.prompts[len(f.agent.prompts)-1]
	if last.Resume == "" || !strings.Contains(last.Prompt, "3 changes; address every one") {
		t.Fatalf("revision prompt = %+v", last)
	}
	// Untimed first, then by time, each with what it is about.
	for i, want := range []string{
		"1. The whole draft:\n   > Daha sakin bir ton",
		"2. The script, at about 4.0 s:\n   > Kancayı hızlandır",
		"3. The visuals, at about 31.5 s:\n   > Grafik yanlış",
	} {
		if !strings.Contains(last.Prompt, want) {
			t.Fatalf("note %d missing from the revision prompt:\n%s", i+1, last.Prompt)
		}
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
	if m := articleD.Measures(); m != (storydomain.Measures{Words: 903}) {
		t.Fatalf("article measures = %+v, want 903 words", m)
	}
	brief = f.agent.prompts[len(f.agent.prompts)-1]
	instructions, _ = os.ReadFile(filepath.Join(brief.Dir, "INSTRUCTIONS.md"))
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
	// English has no writing guide.
	if _, err := os.Stat(filepath.Join(f.agent.prompts[len(f.agent.prompts)-1].Dir, "WRITING.md")); err == nil {
		t.Fatal("the English translation got the Turkish writing guide")
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

func TestVoiceReachesTheTTSTool(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	v, _ := f.voices.Get(ctx, f.ws)
	v.Languages["tr"] = voicedomain.Config{Voice: "okur", Speed: 1, SentencePause: 0.3, ParagraphPause: 0.9, Seed: 42}
	v.Lexicon = map[string]map[string]string{"tr": {"acme": "ek mi"}}
	if _, err := f.voices.Save(ctx, f.ws, id.New(), v); err != nil {
		t.Fatal(err)
	}
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
	def := pipelinedomain.Definition{Name: "S", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source,
		Voice: &voicedomain.Override{Voice: "af_heart"}}
	if _, err := f.pipelines.Create(ctx, f.ws, def); err == nil {
		t.Fatal("an English voice was accepted for a Turkish pipeline")
	}
	def.Voice = &voicedomain.Override{Speed: 1.15}
	p, err := f.pipelines.Create(ctx, f.ws, def)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	f.drain(t)
	var spec struct {
		Speech struct {
			Voice          string            `json:"voice"`
			Speed          float64           `json:"speed"`
			Seed           int               `json:"seed"`
			ParagraphPause float64           `json:"paragraphPause"`
			Lexicon        map[string]string `json:"lexicon"`
		} `json:"speech"`
	}
	for _, e := range f.agent.prompts[len(f.agent.prompts)-1].Env {
		if raw, ok := strings.CutPrefix(e, "KURGU_TOOL_SPEC="); ok {
			_ = json.Unmarshal([]byte(raw), &spec)
		}
	}
	sp := spec.Speech
	if sp.Voice != "okur" || sp.Speed != 1.15 || sp.Seed != 42 || sp.ParagraphPause != 0.9 || sp.Lexicon["acme"] != "ek mi" {
		t.Fatalf("speech spec = %+v", sp)
	}
}

func articleFixture(t *testing.T) (fixture, *storydomain.Story) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t)
	article, _ := pipelinedomain.NewArticleSpec(900, nil)
	outputs, _ := pipelinedomain.NewOutputs(nil, &article, "")
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
	p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "A", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	f.drain(t)
	return f, f.story(t)
}

func TestEditArticleInPlace(t *testing.T) {
	ctx := context.Background()
	f, st := articleFixture(t)
	d := st.Lead()
	c, err := f.svc.Content(ctx, f.ws, st.ID(), d.ID())
	if err != nil || c.Revision != 1 || c.Article == nil || !strings.Contains(*c.Article, "title: \"Kaygı nedir\"") {
		t.Fatalf("content = %+v %v", c, err)
	}
	edited := strings.Replace(*c.Article, "Kaygı nedir", "Kaygı nedir ve ne değildir", 1) + "\nEditörün eklediği paragraf.\r\n"
	editor := id.New()

	bad := []production.Edit{
		{Revision: 1},
		{Revision: 1, Article: ptr("no frontmatter")},
		{Revision: 1, Narration: ptr("bir videoya ait")},
	}
	for i, e := range bad {
		if _, err := f.svc.EditContent(ctx, f.ws, st.ID(), d.ID(), editor, e); !apperror.Is(err, apperror.ValidationFailed) {
			t.Errorf("bad edit %d: %v", i, err)
		}
	}
	if _, err := f.svc.EditContent(ctx, f.ws, st.ID(), d.ID(), editor, production.Edit{Revision: 1, Article: &edited}); err != nil {
		t.Fatal(err)
	}
	st = f.story(t)
	if d = st.Lead(); d.Revision() != 2 || d.Status() != storydomain.StatusReview {
		t.Fatalf("after edit: r%d %s", d.Revision(), d.Status())
	}
	revs, _ := f.svc.Revisions(ctx, f.ws, st.ID(), d.ID())
	if revs[1].EditedBy != editor || revs[1].Artifacts[storydomain.ArtifactArticle] == "" {
		t.Fatalf("revision 2 = %+v", revs[1])
	}
	c, _ = f.svc.Content(ctx, f.ws, st.ID(), d.ID())
	if !strings.Contains(*c.Article, "ne değildir") || strings.Contains(*c.Article, "\r") || !strings.HasSuffix(*c.Article, "paragraf.\n") {
		t.Fatalf("saved article = %q", *c.Article)
	}
	if _, err := f.svc.EditContent(ctx, f.ws, st.ID(), d.ID(), editor, production.Edit{Revision: 1, Article: &edited}); !apperror.Is(err, apperror.Conflict) {
		t.Fatalf("stale edit: %v", err)
	}
}

// A draft cut short by the worker stopping goes back to the runner as an
// error, which releases the job; the next claim drafts again instead of
// leaving the deliverable drafting with no job.
func TestInterruptedDraftRunsAgain(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	article, _ := pipelinedomain.NewArticleSpec(900, nil)
	outputs, _ := pipelinedomain.NewOutputs(nil, &article, "")
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
	p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "A", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	claim := func(kind string) *jobdomain.Job {
		j, err := f.jobs.Claim(ctx, jobdomain.Queues, []string{kind}, "test", time.Minute)
		if err != nil || j == nil {
			t.Fatalf("claim %s = %v, %v", kind, j, err)
		}
		return j
	}
	run := claim(pipelinesvc.KindRun)
	var rp pipelinesvc.RunPayload
	_ = json.Unmarshal(run.Payload(), &rp)
	if err := f.svc.PlanRun(ctx, rp); err != nil {
		t.Fatal(err)
	}
	_ = f.jobs.Complete(ctx, run.ID(), "test", nil)

	draft := claim(production.KindDraft)
	var dp production.DeliverablePayload
	_ = json.Unmarshal(draft.Payload(), &dp)
	jobCtx, stop := context.WithCancel(ctx)
	defer stop()
	f.agent.stop = stop
	if err := f.svc.Draft(jobCtx, dp); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted draft returned %v, want the cancellation", err)
	}
	if d := f.story(t).Lead(); d.Status() != storydomain.StatusDrafting {
		t.Fatalf("after the interruption the %s is %s", d.Kind(), d.Status())
	}
	if err := f.jobs.Release(ctx, draft.ID(), "test"); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if d := f.story(t).Lead(); d.Status() != storydomain.StatusReview || d.Revision() != 1 {
		t.Fatalf("after the rerun: %s r%d", d.Status(), d.Revision())
	}
}

// Every step of a story joins the request that planned it, also those a
// reviewer queues later, outside any job.
func TestStepsJoinTheirRequest(t *testing.T) {
	ctx := context.Background()
	f, st := articleFixture(t)
	fb, _ := storydomain.NewFeedback([]storydomain.Note{{Text: "Girişi kısalt", Scope: storydomain.ScopeAll}}, id.New(), time.Now())
	if _, err := f.svc.RequestChanges(ctx, f.ws, st.ID(), st.Lead().ID(), fb); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	jobs, _, err := f.jobs.List(ctx, f.ws, jobsvc.Filter{})
	if err != nil || len(jobs) != 3 {
		t.Fatalf("jobs = %d, %v", len(jobs), err)
	}
	root := jobs[len(jobs)-1]
	if root.Kind() != pipelinesvc.KindRun || root.TraceID() != root.ID() {
		t.Fatalf("root = %s trace %s", root.Kind(), root.TraceID())
	}
	for _, j := range jobs {
		if j.TraceID() != root.ID() {
			t.Errorf("%s is in trace %s, want %s", j.Kind(), j.TraceID(), root.ID())
		}
	}
}

// selfReviewFixture runs an article pipeline with the given rounds and
// returns the story's lead after drafting.
func selfReviewFixture(t *testing.T, rounds int, setup func(*fakeAgent)) (fixture, *storydomain.Story) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t)
	setup(f.agent)
	article, _ := pipelinedomain.NewArticleSpec(900, nil)
	outputs, _ := pipelinedomain.NewOutputs(nil, &article, "")
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
	p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{
		Name: "A", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source, SelfReview: rounds,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	f.drain(t)
	return f, f.story(t)
}

func lastNotes(t *testing.T, f fixture, st *storydomain.Story) string {
	t.Helper()
	revs, err := f.svc.Revisions(context.Background(), f.ws, st.ID(), st.Lead().ID())
	if err != nil || len(revs) == 0 {
		t.Fatalf("revisions = %d, %v", len(revs), err)
	}
	return revs[len(revs)-1].Notes
}

const reviseOnce = `{"verdict":"revise","summary":"Giriş uzun.","issues":[` +
	`{"file":"article.md","passage":"Kaygı ile ilgili","problem":"The opening repeats itself.","fix":"Cut the second sentence."}]}`

// A fresh, read-only editor reviews the draft; the author fixes what it
// reports and the next round passes.
func TestSelfReviewSendsIssuesBackToTheAuthor(t *testing.T) {
	f, st := selfReviewFixture(t, 2, func(a *fakeAgent) { a.verdicts = []string{reviseOnce} })
	if d := st.Lead(); d.Status() != storydomain.StatusReview || d.Revision() != 1 {
		t.Fatalf("lead = %s r%d", d.Status(), d.Revision())
	}
	if len(f.agent.reviews) != 2 {
		t.Fatalf("editor ran %d times, want 2", len(f.agent.reviews))
	}
	for _, r := range f.agent.reviews {
		if r.Resume != "" || strings.Join(r.Tools, ",") != "Read,Glob,Grep" || !strings.Contains(r.Prompt, "WRITING.md") {
			t.Fatalf("editor request = resume %q tools %v", r.Resume, r.Tools)
		}
	}
	var fix *claude.Request
	for i := range f.agent.prompts {
		if strings.Contains(f.agent.prompts[i].Prompt, "self-review round 1") {
			fix = &f.agent.prompts[i]
		}
	}
	if fix == nil || fix.Resume == "" || !strings.Contains(fix.Prompt, "Cut the second sentence.") {
		t.Fatalf("fix turn = %+v", fix)
	}
	notes := lastNotes(t, f, st)
	for _, want := range []string{"Round 1: 1 to fix. Giriş uzun.", "article.md: The opening repeats itself.", "Round 2: passed."} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
}

// The rounds are a limit: an editor that never passes gets one review
// per round, and the notes say the last fixes went unreviewed.
func TestSelfReviewStopsAtTheRoundLimit(t *testing.T) {
	f, st := selfReviewFixture(t, 1, func(a *fakeAgent) { a.verdicts = []string{reviseOnce, reviseOnce, reviseOnce} })
	if len(f.agent.reviews) != 1 {
		t.Fatalf("editor ran %d times, want 1", len(f.agent.reviews))
	}
	if notes := lastNotes(t, f, st); !strings.Contains(notes, "Round limit reached") {
		t.Fatalf("notes = %s", notes)
	}
	if st.Lead().Status() != storydomain.StatusReview {
		t.Fatalf("lead = %s", st.Lead().Status())
	}
}

func TestSelfReviewOffAndFailing(t *testing.T) {
	f, _ := selfReviewFixture(t, 0, func(*fakeAgent) {})
	if len(f.agent.reviews) != 0 {
		t.Fatalf("editor ran %d times with self-review off", len(f.agent.reviews))
	}
	// An editor that cannot run does not fail the draft.
	f, st := selfReviewFixture(t, 2, func(a *fakeAgent) { a.reviewErr = errors.New("rate limited") })
	if st.Lead().Status() != storydomain.StatusReview {
		t.Fatalf("lead = %s: %s", st.Lead().Status(), st.Lead().Failure())
	}
	if notes := lastNotes(t, f, st); !strings.Contains(notes, "Round 1 could not run: rate limited") {
		t.Fatalf("notes = %s", notes)
	}
}

// A host that drafted an earlier revision keeps its work dir; after a
// newer revision is made elsewhere it must restore it, not resume from
// its stale copy.
func TestStaleWorkDirIsRestored(t *testing.T) {
	ctx := context.Background()
	f, st := articleFixture(t) // revision 1 drafted on f.svc
	d := st.Lead()
	other := f.host()
	c, _ := other.Content(ctx, f.ws, st.ID(), d.ID())
	edited := *c.Article + "\nYalnızca diğer makinede yapılan değişiklik.\n"
	if _, err := other.EditContent(ctx, f.ws, st.ID(), d.ID(), id.New(), production.Edit{Revision: 1, Article: &edited}); err != nil {
		t.Fatal(err)
	}
	fb, _ := storydomain.NewFeedback([]storydomain.Note{{Text: "Girişi kısalt", Scope: storydomain.ScopeAll}}, id.New(), time.Now())
	if _, err := f.svc.RequestChanges(ctx, f.ws, st.ID(), d.ID(), fb); err != nil {
		t.Fatal(err)
	}
	f.drain(t) // back on the first host
	if seen := f.agent.seen[len(f.agent.seen)-1]; !strings.Contains(seen, "diğer makinede") {
		t.Fatalf("the agent resumed from a stale work dir:\n%s", seen)
	}
	last := f.agent.prompts[len(f.agent.prompts)-1]
	if !strings.Contains(last.Prompt, "read the files you are about to change again") {
		t.Fatalf("revise prompt = %s", last.Prompt)
	}
}

func TestEditVideoInPlace(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
	p, _ := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "V", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source})
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	f.drain(t)
	st := f.story(t)
	d := st.Lead()
	c, err := f.svc.Content(ctx, f.ws, st.ID(), d.ID())
	if err != nil || c.Narration == nil || *c.Narration != "Kaygı nedir?" || c.Metadata == nil || c.Metadata.Title != "Kaygı nedir?" {
		t.Fatalf("content = %+v %v", c, err)
	}

	// Metadata only: stays in review and keeps the rendered preview.
	meta := &production.VideoMetadata{Title: "Kaygı nedir, ne değildir?", Description: "Yeni açıklama.", Tags: []string{"kaygı", " ", "endişe"}}
	if _, err := f.svc.EditContent(ctx, f.ws, st.ID(), d.ID(), id.New(), production.Edit{Revision: 1, Metadata: &production.VideoMetadata{Title: "<b>"}}); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("angle brackets accepted: %v", err)
	}
	if _, err := f.svc.EditContent(ctx, f.ws, st.ID(), d.ID(), id.New(), production.Edit{Revision: 1, Metadata: meta, Narration: ptr("Kaygı nedir?\r\n")}); err != nil {
		t.Fatal(err)
	}
	st = f.story(t)
	if d = st.Lead(); d.Revision() != 2 || d.Status() != storydomain.StatusReview {
		t.Fatalf("after metadata edit: r%d %s", d.Revision(), d.Status())
	}
	revs, _ := f.svc.Revisions(ctx, f.ws, st.ID(), d.ID())
	if revs[1].Artifacts[storydomain.ArtifactPreview] != revs[0].Artifacts[storydomain.ArtifactPreview] || revs[1].Artifacts[storydomain.ArtifactPreview] == "" {
		t.Fatalf("preview not kept: %v", revs[1].Artifacts)
	}
	if want := (storydomain.Measures{Duration: 21 * time.Second, Words: 2, Narrator: storydomain.Narrator{Engine: "ema", Voice: "ema"}}); revs[1].Measures != want {
		t.Fatalf("kept preview measures = %+v, want %+v", revs[1].Measures, want)
	}
	c, _ = f.svc.Content(ctx, f.ws, st.ID(), d.ID())
	if c.Metadata.Title != "Kaygı nedir, ne değildir?" || len(c.Metadata.Tags) != 2 {
		t.Fatalf("metadata = %+v", c.Metadata)
	}

	// Narration: back to the agent, which must keep the text and re-time.
	text := "Kaygı, gelecekteki bir tehdide karşı bedenin alarmıdır."
	if _, err := f.svc.EditContent(ctx, f.ws, st.ID(), d.ID(), id.New(), production.Edit{Revision: 2, Narration: &text}); err != nil {
		t.Fatal(err)
	}
	if d = f.story(t).Lead(); d.Status() != storydomain.StatusQueued || d.Revision() != 3 {
		t.Fatalf("after narration edit: r%d %s", d.Revision(), d.Status())
	}
	// Not rendered yet: the edited words, but no length; the voice stays.
	if m := d.Measures(); m != (storydomain.Measures{Words: 7, Narrator: storydomain.Narrator{Engine: "ema", Voice: "ema"}}) {
		t.Fatalf("after narration edit measures = %+v, want 7 words", m)
	}
	f.drain(t)
	last := f.agent.prompts[len(f.agent.prompts)-1]
	if last.Resume == "" || !strings.Contains(last.Prompt, "word for word") {
		t.Fatalf("retime prompt = %+v", last)
	}
	if seen := f.agent.seen[len(f.agent.seen)-1]; strings.TrimSpace(seen) != text {
		t.Fatalf("the agent found narration %q", seen)
	}
	if d = f.story(t).Lead(); d.Status() != storydomain.StatusReview || d.Revision() != 4 {
		t.Fatalf("after re-timing: r%d %s", d.Revision(), d.Status())
	}
}

func ptr[T any](v T) *T { return &v }

// crashingPublisher dies mid-publication while crashes lasts, like a
// worker killed during the upload; after that it publishes.
type crashingPublisher struct {
	fakePublisher
	crashes *int
}

func (p crashingPublisher) Publish(ctx context.Context, req production.PublishRequest) (storydomain.Publication, error) {
	if *p.crashes > 0 {
		*p.crashes--
		panic("worker killed")
	}
	return p.fakePublisher.Publish(ctx, req)
}

// A publication cut short leaves the deliverable publishing; the retry of
// the same job resumes it, and no other job may publish it meanwhile.
func TestCutShortPublicationResumesOnItsJob(t *testing.T) {
	ctx := context.Background()
	f, st := articleFixture(t)
	d := st.Lead()
	crashes := 1
	f.svc.AddPublisher(crashingPublisher{fakePublisher{pipelinedomain.KindArticle}, &crashes})
	if _, err := f.svc.Approve(ctx, f.ws, st.ID(), d.ID(), id.New()); err != nil {
		t.Fatal(err)
	}
	j, err := f.jobs.Claim(ctx, jobdomain.Queues, []string{production.KindPublish}, "test", time.Minute)
	if err != nil || j == nil {
		t.Fatalf("claim publish = %v, %v", j, err)
	}
	p := production.DeliverablePayload{StoryID: st.ID(), DeliverableID: d.ID()}
	func() {
		defer func() { _ = recover() }()
		_ = f.svc.PublishJob(ctx, j.ID(), p)
	}()
	if got := f.story(t).Lead(); got.Status() != storydomain.StatusPublishing {
		t.Fatalf("after the crash the article is %s", got.Status())
	}

	if err := f.svc.PublishJob(ctx, id.New(), p); !errors.Is(err, jobdomain.ErrPermanent) {
		t.Fatalf("another job publishing = %v, want a permanent refusal", err)
	}
	if err := f.svc.PublishJob(ctx, j.ID(), p); err != nil {
		t.Fatalf("retry of the same job = %v", err)
	}
	if got := f.story(t).Lead(); got.Status() != storydomain.StatusPublished || got.Publication() == nil {
		t.Fatalf("after the retry the article is %s", got.Status())
	}
}

// A draft whose job ends without finishing it (its worker vanished, it
// was cancelled, or it failed outright) fails the deliverable, which a
// person can then retry, instead of leaving it waiting for a job that is
// gone.
func TestDraftWhoseJobEndsCanBeRetried(t *testing.T) {
	tests := []struct {
		name  string
		begun bool // the worker marked the deliverable drafting
		end   func(f fixture, j *jobdomain.Job) error
	}{
		{name: "worker vanished mid-draft", begun: true, end: func(f fixture, _ *jobdomain.Job) error {
			later := time.Now().Add(time.Hour)
			f.jobs.WithClock(func() time.Time { return later })
			_, err := f.jobs.ExpireAbandoned(context.Background())
			return err
		}},
		{name: "cancelled before it began", end: func(f fixture, j *jobdomain.Job) error {
			_, err := f.jobs.Cancel(context.Background(), f.ws, j.ID())
			return err
		}},
		{name: "failed outright", begun: true, end: func(f fixture, j *jobdomain.Job) error {
			return f.jobs.Complete(context.Background(), j.ID(), "test", errors.New("disk full"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			article, _ := pipelinedomain.NewArticleSpec(900, nil)
			outputs, _ := pipelinedomain.NewOutputs(nil, &article, "")
			source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
			p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "A", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
			run, _ := f.jobs.Claim(ctx, jobdomain.Queues, []string{pipelinesvc.KindRun}, "test", time.Minute)
			var rp pipelinesvc.RunPayload
			_ = json.Unmarshal(run.Payload(), &rp)
			if err := f.svc.PlanRun(ctx, rp); err != nil {
				t.Fatal(err)
			}
			_ = f.jobs.Complete(ctx, run.ID(), "test", nil)

			draft, err := f.jobs.Claim(ctx, jobdomain.Queues, []string{production.KindDraft}, "test", time.Minute)
			if err != nil || draft == nil {
				t.Fatalf("claim draft = %v, %v", draft, err)
			}
			if tt.begun {
				var dp production.DeliverablePayload
				_ = json.Unmarshal(draft.Payload(), &dp)
				if _, _, err := f.svc.BeginDraft(ctx, dp); err != nil {
					t.Fatal(err)
				}
			}
			if err := tt.end(f, draft); err != nil {
				t.Fatal(err)
			}
			st := f.story(t)
			d := st.Lead()
			if d.Status() != storydomain.StatusFailed {
				t.Fatalf("after the job ended the article is %s", d.Status())
			}
			if _, err := f.svc.Retry(ctx, f.ws, st.ID(), d.ID()); err != nil {
				t.Fatal(err)
			}
			f.drain(t)
			if d := f.story(t).Lead(); d.Status() != storydomain.StatusReview || d.Revision() != 1 {
				t.Fatalf("after the retry: %s r%d", d.Status(), d.Revision())
			}
		})
	}
}

// interleavedBlobs runs before once, ahead of the first upload whose key
// contains match.
type interleavedBlobs struct {
	blob.Store
	match  string
	before func()
	once   sync.Once
}

func (b *interleavedBlobs) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if strings.Contains(key, b.match) {
		b.once.Do(b.before)
	}
	return b.Store.Put(ctx, key, r, size)
}

// Two reviewers edit the same revision at once: one is saved, the other
// refused, and the saved revision keeps its own files even though the
// refused edit uploaded after it was saved.
func TestConcurrentEditsKeepTheSavedOnesFiles(t *testing.T) {
	ctx := context.Background()
	f, st := articleFixture(t)
	d := st.Lead()
	c, err := f.svc.Content(ctx, f.ws, st.ID(), d.ID())
	if err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(*c.Article, "Kaygı nedir", "Kaygı nedir (benim)", 1)
	theirs := strings.Replace(*c.Article, "Kaygı nedir", "Kaygı nedir (onların)", 1)
	slow := f.hostWith(func(deps *production.Deps) {
		deps.Blobs = &interleavedBlobs{Store: f.blobs, match: "/r2/", before: func() {
			// The other reviewer saves while this edit is still uploading.
			if _, err := f.svc.EditContent(ctx, f.ws, st.ID(), d.ID(), id.New(), production.Edit{Revision: 1, Article: &theirs}); err != nil {
				t.Errorf("their edit: %v", err)
			}
		}}
	})
	if _, err := slow.EditContent(ctx, f.ws, st.ID(), d.ID(), id.New(), production.Edit{Revision: 1, Article: &mine}); !apperror.Is(err, apperror.Conflict) {
		t.Fatalf("the later save = %v, want a conflict", err)
	}
	got, err := f.svc.Content(ctx, f.ws, st.ID(), d.ID())
	if err != nil || got.Revision != 2 {
		t.Fatalf("content = r%d %v", got.Revision, err)
	}
	if !strings.Contains(*got.Article, "(onların)") {
		t.Fatalf("revision 2 holds the refused edit:\n%s", *got.Article)
	}
}

// A keyword another run takes while this run's agent writes ideas for it
// does not get a second story.
func TestKeywordTakenMeanwhileGetsNoSecondStory(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.seo.Import(ctx, f.ws, []seosvc.Item{{Keyword: "ruminasyon nedir", Language: "tr", Volume: 2400}}, "manual"); err != nil {
		t.Fatal(err)
	}
	open, _ := f.seo.Next(ctx, f.ws, "tr", 1)
	other := id.New()
	f.agent.onIdeas = func() {
		if ok, err := f.seo.Claim(ctx, f.ws, open[0].ID, other); !ok || err != nil {
			t.Errorf("the other run's claim = %v, %v", ok, err)
		}
	}
	video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
	outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
	source, _ := pipelinedomain.NewSEOSource("Arama yapan yetişkinlere eğitici kısa videolar.")
	p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "SEO", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	run, _ := f.jobs.Claim(ctx, jobdomain.Queues, []string{pipelinesvc.KindRun}, "test", time.Minute)
	var rp pipelinesvc.RunPayload
	_ = json.Unmarshal(run.Payload(), &rp)
	if err := f.svc.PlanRun(ctx, rp); !errors.Is(err, jobdomain.ErrPermanent) {
		t.Fatalf("plan = %v, want a permanent failure: nothing left to write", err)
	}
	if list, _, _ := f.svc.List(ctx, f.ws, production.Filter{}); len(list) != 0 {
		t.Fatalf("%d stories for a keyword another run took", len(list))
	}
	used, _, _ := f.seo.List(ctx, f.ws, seosvc.Filter{Statuses: []seodomain.Status{seodomain.StatusUsed}})
	if len(used) != 1 || used[0].StoryID != other {
		t.Fatalf("used = %+v", used)
	}
}

// failingEnqueuer refuses jobs of one kind, like a queue write failing.
type failingEnqueuer struct {
	production.Enqueuer
	kind string
}

func (e failingEnqueuer) Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error) {
	if spec.Kind == e.kind {
		return nil, false, errors.New("queue unavailable")
	}
	return e.Enqueuer.Enqueue(ctx, spec)
}

type brokenPublisher struct{ fakePublisher }

func (brokenPublisher) Publish(context.Context, production.PublishRequest) (storydomain.Publication, error) {
	return storydomain.Publication{}, errors.New("blog unreachable")
}

// A decision whose follow-up job cannot be queued is not saved either:
// the deliverable never sits ready with no publication queued.
func TestDecisionAndItsFollowUpAreOneTransaction(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, f fixture, st *storydomain.Story) // from an article in review
		decide func(svc *production.Service, f fixture, st *storydomain.Story) error
		want   storydomain.Status
	}{
		{
			name: "approve",
			decide: func(svc *production.Service, f fixture, st *storydomain.Story) error {
				_, err := svc.Approve(context.Background(), f.ws, st.ID(), st.Lead().ID(), id.New())
				return err
			},
			want: storydomain.StatusReview,
		},
		{
			name: "retry a failed publication",
			setup: func(t *testing.T, f fixture, st *storydomain.Story) {
				broken := f.host()
				broken.AddPublisher(brokenPublisher{fakePublisher{pipelinedomain.KindArticle}})
				if _, err := broken.Approve(context.Background(), f.ws, st.ID(), st.Lead().ID(), id.New()); err != nil {
					t.Fatal(err)
				}
				f.drainOn(t, broken)
			},
			decide: func(svc *production.Service, f fixture, st *storydomain.Story) error {
				_, err := svc.Retry(context.Background(), f.ws, st.ID(), st.Lead().ID())
				return err
			},
			want: storydomain.StatusFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, st := articleFixture(t)
			if tt.setup != nil {
				tt.setup(t, f, st)
			}
			flaky := f.hostWith(func(deps *production.Deps) {
				deps.Jobs = failingEnqueuer{Enqueuer: deps.Jobs, kind: production.KindPublish}
			})
			if err := tt.decide(flaky, f, st); err == nil {
				t.Fatal("the decision succeeded though its publication could not be queued")
			}
			if got := f.story(t).Lead(); got.Status() != tt.want {
				t.Fatalf("the article is %s, want it left %s", got.Status(), tt.want)
			}
		})
	}
}

// racingStories runs race once, right after the next Get.
type racingStories struct {
	storyrepo.Repository
	race func()
}

func (r *racingStories) Get(ctx context.Context, ws, id uuid.UUID) (*storydomain.Story, error) {
	st, err := r.Repository.Get(ctx, ws, id)
	if race := r.race; race != nil {
		r.race = nil
		race()
	}
	return st, err
}

// A worker starting a draft while the story is being deleted either
// finds it gone or stops the deletion: never drafts a deleted story.
func TestDeleteRacingADraftNeverOrphansIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	article, _ := pipelinedomain.NewArticleSpec(900, nil)
	outputs, _ := pipelinedomain.NewOutputs(nil, &article, "")
	source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
	p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "A", Language: pipelinedomain.Turkish, Outputs: outputs, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
	run, _ := f.jobs.Claim(ctx, jobdomain.Queues, []string{pipelinesvc.KindRun}, "test", time.Minute)
	var rp pipelinesvc.RunPayload
	_ = json.Unmarshal(run.Payload(), &rp)
	if err := f.svc.PlanRun(ctx, rp); err != nil {
		t.Fatal(err)
	}
	st := f.story(t)
	dp := production.DeliverablePayload{StoryID: st.ID(), DeliverableID: st.Lead().ID()}

	began := make(chan error, 1)
	var stories *racingStories
	deleter := f.hostWith(func(deps *production.Deps) {
		stories = &racingStories{Repository: deps.Stories}
		deps.Stories = stories
	})
	stories.race = func() {
		// The worker starts its draft as the story is being deleted; it
		// gets the time it needs unless the deletion holds it off.
		go func() {
			_, _, err := f.svc.BeginDraft(context.Background(), dp)
			began <- err
		}()
		select {
		case err := <-began:
			began <- err
		case <-time.After(300 * time.Millisecond):
		}
	}
	delErr := deleter.Delete(ctx, f.ws, st.ID())
	beginErr := <-began
	switch {
	case delErr == nil && beginErr == nil:
		t.Fatal("the story was deleted while its draft began")
	case delErr == nil && !errors.Is(beginErr, jobdomain.ErrPermanent):
		t.Fatalf("the draft of a deleted story = %v, want a permanent refusal", beginErr)
	case delErr != nil && !apperror.Is(delErr, apperror.Conflict):
		t.Fatalf("delete = %v", delErr)
	}
}

// A pipeline's or request's drafting model and effort reach every agent
// turn of a draft (the author, the editor and the author's fix), while
// idea planning keeps the workspace default.
func TestDraftingAgentReachesEveryTurn(t *testing.T) {
	ctx := context.Background()
	agent := pipelinedomain.Agent{Model: "claude-sonnet-5-5", Effort: pipelinedomain.EffortLow}
	article, _ := pipelinedomain.NewArticleSpec(900, nil)
	outputs, _ := pipelinedomain.NewOutputs(nil, &article, "")
	check := func(t *testing.T, f fixture) {
		t.Helper()
		drafting := 0
		for _, r := range f.agent.prompts {
			if strings.Contains(string(r.JSONSchema), `"ideas"`) {
				if r.Model != "" || r.Effort != "" {
					t.Errorf("idea planning ran with %q/%q", r.Model, r.Effort)
				}
				continue
			}
			drafting++
			if r.Model != agent.Model || r.Effort != string(agent.Effort) {
				t.Errorf("turn %.40q ran with %q/%q", r.Prompt, r.Model, r.Effort)
			}
		}
		if drafting < 3 {
			t.Fatalf("only %d drafting turns ran", drafting)
		}
	}

	t.Run("pipeline", func(t *testing.T) {
		f := newFixture(t)
		f.agent.verdicts = []string{reviseOnce}
		source, _ := pipelinedomain.NewPromptSource("Kaygı üzerine.")
		p, err := f.pipelines.Create(ctx, f.ws, pipelinedomain.Definition{Name: "A", Language: pipelinedomain.Turkish,
			Outputs: outputs, Source: source, SelfReview: 1, Production: pipelinedomain.Production{Agent: agent}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.pipelines.RunNow(ctx, f.ws, p.ID(), 1)
		f.drain(t)
		if got := f.story(t).Brief().Production.Agent; got != agent {
			t.Fatalf("brief agent = %+v", got)
		}
		check(t, f)
	})

	t.Run("ad-hoc request", func(t *testing.T) {
		f := newFixture(t)
		f.agent.verdicts = []string{reviseOnce}
		req := production.AdHocRequest{Idea: "Kaygı ve endişe farkı", Count: 1, Language: pipelinedomain.Turkish,
			Outputs: outputs, SelfReview: 1, Production: pipelinedomain.Production{Agent: agent}}
		if _, err := f.svc.QueueAdHoc(ctx, f.ws, id.New(), req); err != nil {
			t.Fatal(err)
		}
		f.drain(t)
		check(t, f)
		req.Production.Agent.Effort = "turbo"
		if _, err := f.svc.QueueAdHoc(ctx, f.ws, id.New(), req); !apperror.Is(err, apperror.ValidationFailed) {
			t.Fatalf("unknown effort: err = %v", err)
		}
	})
}

// A story's preview quality sets the scale of its review render; a
// preview at final quality is approved as the final video without
// rendering it again.
func TestPreviewQuality(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		quality     pipelinedomain.PreviewQuality
		wantRenders []float64 // preview, then final if one runs
	}{
		{quality: "", wantRenders: []float64{0.5, 1}},
		{quality: pipelinedomain.Preview720p, wantRenders: []float64{2.0 / 3, 1}},
		{quality: pipelinedomain.Preview1080p, wantRenders: []float64{1}},
	}
	for _, tt := range tests {
		t.Run(cmp.Or(string(tt.quality), "default"), func(t *testing.T) {
			f := newFixture(t)
			log := &scaleLog{}
			svc := f.hostWith(func(d *production.Deps) { d.Renderer = fakeRenderer{scales: log} })
			video, _ := pipelinedomain.NewVideoSpec(pipelinedomain.FormatShort, pipelinedomain.Vertical, 20*time.Second)
			outputs, _ := pipelinedomain.NewOutputs(&video, nil, "")
			req := production.AdHocRequest{Idea: "Kaygı ve endişe farkı", Count: 1, Language: pipelinedomain.Turkish,
				Outputs: outputs, Production: pipelinedomain.Production{PreviewQuality: tt.quality}}
			if _, err := svc.QueueAdHoc(ctx, f.ws, id.New(), req); err != nil {
				t.Fatal(err)
			}
			f.drainOn(t, svc)
			st := f.story(t)
			if st.Lead().Status() != storydomain.StatusReview {
				t.Fatalf("lead = %s: %s", st.Lead().Status(), st.Lead().Failure())
			}
			if _, err := svc.Approve(ctx, f.ws, st.ID(), st.Lead().ID(), id.New()); err != nil {
				t.Fatal(err)
			}
			f.drainOn(t, svc)
			if got := log.all(); !slices.Equal(got, tt.wantRenders) {
				t.Fatalf("rendered at %v, want %v", got, tt.wantRenders)
			}
			st = f.story(t)
			if d := st.Lead(); d.Status() != storydomain.StatusReady && d.Status() != storydomain.StatusPublished {
				t.Fatalf("lead = %s: %s", d.Status(), d.Failure())
			}
			revs, err := svc.Revisions(ctx, f.ws, st.ID(), st.Lead().ID())
			if err != nil {
				t.Fatal(err)
			}
			last := revs[len(revs)-1].Artifacts
			if last[storydomain.ArtifactFinal] == "" {
				t.Fatalf("no final artifact: %v", last)
			}
			reused := last[storydomain.ArtifactFinal] == last[storydomain.ArtifactPreview]
			if reused != (len(tt.wantRenders) == 1) {
				t.Fatalf("final reused the preview = %v; artifacts %v", reused, last)
			}
		})
	}
}
