package production_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/adapter/claude"
	"github.com/ahmethakanbesel/ekin/internal/agenttool"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	pipelinedomain "github.com/ahmethakanbesel/ekin/internal/domain/pipeline/v1"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	siterepo "github.com/ahmethakanbesel/ekin/internal/repository/site/v1"
	production "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	sitesvc "github.com/ahmethakanbesel/ekin/internal/service/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// changeLog records what change drafts found and did.
var changeLog struct {
	mu   sync.Mutex
	seen []string // about.html as each draft found it
}

// change plays the agent on a change: it edits repo/about.html (adding
// a line per round), runs the build tool as told, and writes a summary.
func (a *fakeAgent) change(ctx context.Context, req claude.Request) (claude.Result, error) {
	about := filepath.Join(req.Dir, agenttool.RepoDir, "about.html")
	before, _ := os.ReadFile(about) //nolint:gosec // the test's work dir
	changeLog.mu.Lock()
	changeLog.seen = append(changeLog.seen, string(before))
	changeLog.mu.Unlock()
	if err := os.WriteFile(about, append(before, []byte("<meta name=\"description\" content=\"About us\">\n")...), 0o600); err != nil {
		return claude.Result{}, err
	}
	_ = os.WriteFile(filepath.Join(req.Dir, agenttool.SummaryFile), []byte("Added the description. Fixes finding 1."), 0o600)
	env := agenttool.Env{Dir: req.Dir, Stdout: io.Discard, Stderr: io.Discard, Spec: toolSpec(req)}
	_ = agenttool.Run(ctx, env, []string{"build"})
	return claude.Result{SessionID: "change-session", Text: "Added the description."}, nil
}

// fakeRepos copies a two-page site.
type fakeRepos struct{ snapshots *int }

func (r fakeRepos) Snapshot(_ context.Context, _ uuid.UUID, dest string) (production.ChangeBase, error) {
	*r.snapshots++
	return production.ChangeBase{Source: "github", Repo: "o/site", Branch: "main", SHA: "abc123"}, writeSite(dest)
}

func (r fakeRepos) SnapshotAt(_ context.Context, _ uuid.UUID, base production.ChangeBase, dest string) error {
	if base.SHA != "abc123" {
		return os.ErrNotExist
	}
	*r.snapshots++
	return writeSite(dest)
}

func writeSite(dest string) error {
	for name, body := range map[string]string{"index.html": "<title>Home</title>\n", "about.html": "<title>About</title>\n"} {
		if err := os.WriteFile(filepath.Join(dest, name), []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

type changeFixture struct {
	fixture
	sites     *sitesvc.Service
	siteRepo  *siterepo.InMemory
	svc       *production.Service
	host      func() *production.Service
	snapshots *int
	findings  map[sitedomain.Rule]*sitedomain.Finding
}

func newChangeFixture(t *testing.T, build string) changeFixture {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t)
	repo := siterepo.NewInMemory()
	sites := sitesvc.NewService(repo, &db.MemTxManager{}, nil, nil, sitesvc.Config{}, slog.New(slog.DiscardHandler))
	site, err := sitedomain.New(f.ws, sitedomain.Settings{URL: "https://example.com", Schedule: sitedomain.ScheduleOff, Build: build}, id.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.PutSite(ctx, site); err != nil {
		t.Fatal(err)
	}
	raised := []sitedomain.Raised{
		{Rule: sitedomain.RuleDescMissing, URL: "https://example.com/about"},
		{Rule: sitedomain.RuleDescMissing, URL: "https://example.com/team"},
		{Rule: sitedomain.RuleTitleMissing, URL: "https://example.com/blank", Evidence: []string{"no <title>"}},
	}
	all := sitedomain.Reconcile(nil, raised, f.ws, id.New(), id.New, time.Now())
	if err := repo.PutFindings(ctx, all); err != nil {
		t.Fatal(err)
	}
	_ = repo.ReplacePages(ctx, f.ws, []sitedomain.Page{{URL: "https://example.com/about", Status: 200, Title: "About"}})
	n := 0
	host := func() *production.Service {
		svc := f.hostWith(func(d *production.Deps) { d.Sites, d.Repos = sites, fakeRepos{snapshots: &n} })
		return svc.AddPublisher(fakePublisher{kind: pipelinedomain.KindChange})
	}
	cf := changeFixture{fixture: f, sites: sites, siteRepo: repo, svc: host(), host: host, snapshots: &n,
		findings: map[sitedomain.Rule]*sitedomain.Finding{}}
	for _, x := range all {
		if x.URL() != "https://example.com/team" {
			cf.findings[x.Rule()] = x
		}
	}
	return cf
}

func (cf changeFixture) claimedBy(t *testing.T, r sitedomain.Rule) uuid.UUID {
	t.Helper()
	got, err := cf.siteRepo.Finding(context.Background(), cf.ws, cf.findings[r].ID())
	if err != nil {
		t.Fatal(err)
	}
	return got.ClaimedBy()
}

func TestChangeFromFindingsToPublication(t *testing.T) {
	ctx := context.Background()
	cf := newChangeFixture(t, "grep -q title about.html")
	desc := cf.findings[sitedomain.RuleDescMissing]
	st, err := cf.svc.QueueChange(ctx, cf.ws, id.New(), production.ChangeRequest{FindingIDs: []uuid.UUID{desc.ID()}, Instructions: "Keep it short."})
	if err != nil {
		t.Fatal(err)
	}
	if st.Idea().Title != "Add missing meta descriptions" || !st.Lead().IsChange() || cf.claimedBy(t, sitedomain.RuleDescMissing) != st.ID() {
		t.Fatalf("change story %q, claimed by %v", st.Idea().Title, cf.claimedBy(t, sitedomain.RuleDescMissing))
	}
	cf.drainOn(t, cf.svc)

	got, _ := cf.svc.Get(ctx, cf.ws, st.ID())
	d := got.Lead()
	if d.Status() != storydomain.StatusReview || d.Revision() != 1 {
		t.Fatalf("after the draft: %s r%d (%s)", d.Status(), d.Revision(), d.Failure())
	}
	req := cf.agent.prompts[len(cf.agent.prompts)-1]
	allowed := strings.Join(req.Allowed, ",")
	if !strings.Contains(allowed, "Bash(ekin tool build)") || !strings.Contains(allowed, "Bash(ekin tool diff)") || strings.Contains(allowed, "tool:*") {
		t.Fatalf("change agent may run %s", allowed)
	}
	brief, _ := os.ReadFile(filepath.Join(req.Dir, "BRIEF.md"))
	for _, want := range []string{"o/site, branch main at abc123", "Add missing meta descriptions", "https://example.com/about", "Keep it short.", `title "About"`} {
		if !strings.Contains(string(brief), want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
	revs, _ := cf.svc.Revisions(ctx, cf.ws, st.ID(), d.ID())
	for _, name := range []string{storydomain.ArtifactPatch, storydomain.ArtifactChange, storydomain.ArtifactChangeFiles, storydomain.ArtifactSummary, storydomain.ArtifactWorkdir} {
		if revs[0].Artifacts[name] == "" {
			t.Fatalf("revision 1 lacks %s: %v", name, revs[0].Artifacts)
		}
	}
	rc, _, _ := cf.svc.Artifact(ctx, cf.ws, st.ID(), d.ID(), 1, storydomain.ArtifactChange)
	var m production.ChangeManifest
	_ = json.NewDecoder(rc).Decode(&m)
	_ = rc.Close()
	if m.Base.SHA != "abc123" || len(m.Files) != 1 || m.Files[0].Path != "about.html" || m.Files[0].Additions != 1 || m.Build == nil || !m.Build.OK {
		t.Fatalf("manifest = %+v", m)
	}

	// Feedback resumes the session on the same copy, with the change so far.
	fb, _ := storydomain.NewFeedback([]storydomain.Note{{Text: "Mention the team."}}, id.New(), time.Now())
	if _, err := cf.svc.RequestChanges(ctx, cf.ws, st.ID(), d.ID(), fb); err != nil {
		t.Fatal(err)
	}
	cf.drainOn(t, cf.svc)
	req = cf.agent.prompts[len(cf.agent.prompts)-1]
	if req.Resume != "change-session" || !strings.Contains(req.Prompt, "revision 1 of your change") {
		t.Fatalf("revision 2 request: resume %q, prompt %q", req.Resume, req.Prompt)
	}
	// Another host, without the work dir, rebuilds the copy from the base
	// commit and the last revision's patch.
	other := cf.host()
	if _, err := other.RequestChanges(ctx, cf.ws, st.ID(), d.ID(), fb); err != nil {
		t.Fatal(err)
	}
	cf.drainOn(t, other)
	changeLog.mu.Lock()
	seen := changeLog.seen[len(changeLog.seen)-1]
	changeLog.mu.Unlock()
	if strings.Count(seen, "description") != 2 || *cf.snapshots != 2 {
		t.Fatalf("the other host found about.html as %q after %d copies", seen, *cf.snapshots)
	}

	if _, err := cf.svc.Approve(ctx, cf.ws, st.ID(), d.ID(), id.New()); err != nil {
		t.Fatal(err)
	}
	cf.drainOn(t, cf.svc)
	got, _ = cf.svc.Get(ctx, cf.ws, st.ID())
	if got.Lead().Status() != storydomain.StatusPublished {
		t.Fatalf("approved change is %s: %s", got.Lead().Status(), got.Lead().Failure())
	}
	// Its finding stays claimed until a crawl no longer raises it.
	if _, err := cf.svc.QueueChange(ctx, cf.ws, id.New(), production.ChangeRequest{FindingIDs: []uuid.UUID{desc.ID()}}); !apperror.Is(err, apperror.Conflict) {
		t.Fatalf("a second change took a claimed finding: %v", err)
	}
}

func TestRejectedChangeReleasesItsFindings(t *testing.T) {
	ctx := context.Background()
	cf := newChangeFixture(t, "")
	title := cf.findings[sitedomain.RuleTitleMissing]
	st, err := cf.svc.QueueChange(ctx, cf.ws, id.New(), production.ChangeRequest{FindingIDs: []uuid.UUID{title.ID()}})
	if err != nil {
		t.Fatal(err)
	}
	cf.drainOn(t, cf.svc)
	if _, err := cf.svc.Reject(ctx, cf.ws, st.ID(), st.Lead().ID()); err != nil {
		t.Fatal(err)
	}
	if by := cf.claimedBy(t, sitedomain.RuleTitleMissing); by != uuid.Nil {
		t.Fatalf("rejected change still holds its finding: %v", by)
	}
	if _, err := cf.svc.QueueChange(ctx, cf.ws, id.New(), production.ChangeRequest{}); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("an empty change request: %v", err)
	}
	prompt, err := cf.svc.QueueChange(ctx, cf.ws, id.New(), production.ChangeRequest{Instructions: "Add a pricing comparison page against Acme\nwith a table."})
	if err != nil || prompt.Idea().Title != "Add a pricing comparison page against Acme" || len(prompt.Brief().Findings) != 0 {
		t.Fatalf("change from instructions: %v %q", err, prompt.Idea().Title)
	}
	if err := cf.svc.Delete(ctx, cf.ws, prompt.ID()); err != nil && !apperror.Is(err, apperror.Conflict) {
		t.Fatal(err)
	}
}

func TestChangeWhoseBuildFailsFails(t *testing.T) {
	ctx := context.Background()
	cf := newChangeFixture(t, "echo 'error: broken layout' >&2; exit 2")
	st, err := cf.svc.QueueChange(ctx, cf.ws, id.New(), production.ChangeRequest{Instructions: "Tidy the about page."})
	if err != nil {
		t.Fatal(err)
	}
	cf.drainOn(t, cf.svc)
	got, _ := cf.svc.Get(ctx, cf.ws, st.ID())
	if d := got.Lead(); d.Status() != storydomain.StatusFailed || !strings.Contains(d.Failure(), "broken layout") {
		t.Fatalf("a change whose build fails is %s: %q", d.Status(), d.Failure())
	}
	// The agent was told why, twice, before the draft failed.
	fixes := 0
	for _, p := range cf.agent.prompts {
		if strings.Contains(p.Prompt, "the build fails on these edits") {
			fixes++
		}
	}
	if fixes != 2 {
		t.Fatalf("the agent was asked to fix the build %d times", fixes)
	}
}

func TestFindingsPipelinePlansOneChangePerRule(t *testing.T) {
	ctx := context.Background()
	cf := newChangeFixture(t, "")
	source, _ := pipelinedomain.NewFindingsSource("")
	p, err := cf.pipelines.Create(ctx, cf.ws, pipelinedomain.Definition{Name: "Fix the site", Language: pipelinedomain.English,
		Outputs: pipelinedomain.NewChangeOutputs(), Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cf.pipelines.RunNow(ctx, cf.ws, p.ID(), 3); err != nil {
		t.Fatal(err)
	}
	cf.drainOn(t, cf.svc)
	list, _, _ := cf.svc.List(ctx, cf.ws, production.Filter{})
	titles := map[string]int{}
	for _, st := range list {
		titles[st.Idea().Title] = len(st.Brief().Findings)
		if st.PipelineID() != p.ID() || st.Lead().Status() != storydomain.StatusReview {
			t.Fatalf("pipeline change %q: %s", st.Idea().Title, st.Lead().Status())
		}
	}
	// The most severe rule first (missing titles are errors), each rule
	// once, and no third story when nothing is left.
	if len(list) != 2 || titles["Add missing page titles"] != 1 || titles["Add missing meta descriptions (2 pages)"] != 2 {
		t.Fatalf("planned changes = %v", titles)
	}
}
