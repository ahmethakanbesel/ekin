// Package seo manages the keyword-opportunity pool: import, refresh from
// OpenSEO through the agent, and hand opportunities to pipelines.
package seo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/claude"
	"kurgu/internal/adapter/openseo"
	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	seodomain "kurgu/internal/domain/seo/v1"
	seorepo "kurgu/internal/repository/seo/v1"
	settingsrepo "kurgu/internal/repository/settings/v1"
	"kurgu/pkg/id"
)

// KindRefresh is the job that asks OpenSEO for new opportunities.
const KindRefresh = "seo.refresh"

// RefreshPayload is the seo.refresh job payload.
type RefreshPayload struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	Count       int       `json:"count"`
}

// Agent runs Claude Code (internal/adapter/claude).
type Agent interface {
	Run(ctx context.Context, req claude.Request) (claude.Result, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// Research is OpenSEO called directly with an API key
// (internal/adapter/openseo).
type Research interface {
	Whoami(ctx context.Context) (openseo.Account, error)
	Projects(ctx context.Context) ([]openseo.Project, error)
	ResearchKeywords(ctx context.Context, projectID string, seeds []string, m openseo.Market) ([]openseo.SeedResult, error)
	DomainKeywords(ctx context.Context, projectID, domain string, m openseo.Market) ([]openseo.Keyword, error)
}

// Service runs SEO use cases.
type Service struct {
	research func(ctx context.Context) Research
	repo     seorepo.Repository
	settings settingsrepo.Repository
	jobs     Enqueuer
	agent    Agent
	workDir  string
	now      func() time.Time
}

// NewService returns a Service.
func NewService(repo seorepo.Repository, settings settingsrepo.Repository, jobs Enqueuer, agent Agent, workDir string) *Service {
	return &Service{repo: repo, settings: settings, jobs: jobs, agent: agent, workDir: workDir, now: time.Now}
}

// WithResearch lets refreshes call OpenSEO directly. research returns
// nil while no API key is set; refreshes then go through the agent and
// the host's Claude Code OpenSEO plugin.
func (s *Service) WithResearch(research func(ctx context.Context) Research) *Service {
	s.research = research
	return s
}

func (s *Service) direct(ctx context.Context) Research {
	if s.research == nil {
		return nil
	}
	return s.research(ctx)
}

// Item is an opportunity to import.
type Item struct {
	Keyword    string
	Language   string
	Volume     int
	Difficulty int
	Intent     string
	Note       string
}

// Import adds items to the pool, skipping keywords already there, and
// returns how many were added.
func (s *Service) Import(ctx context.Context, workspaceID uuid.UUID, items []Item, source string) (int, error) {
	if len(items) > 500 {
		return 0, apperror.New(apperror.ValidationFailed, "import at most 500 keywords at a time")
	}
	added := 0
	for _, it := range items {
		o, err := seodomain.New(id.New(), workspaceID, it.Keyword, it.Language, it.Volume, it.Difficulty, it.Intent, it.Note, source, s.now())
		if err != nil {
			return added, apperror.Wrap(apperror.ValidationFailed, err, "keyword "+it.Keyword)
		}
		ok, err := s.repo.Add(ctx, o)
		if err != nil {
			return added, err
		}
		if ok {
			added++
		}
	}
	return added, nil
}

// Filter narrows a listing of the pool and picks its page.
type Filter = seorepo.Filter

// List returns a page of the pool, open first, best first, and the cursor
// of the next page ("" after the last).
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID, f Filter) ([]seodomain.Opportunity, string, error) {
	return s.repo.List(ctx, workspaceID, f)
}

// SetStatus dismisses or reopens an opportunity. One that a run took
// meanwhile fails with Conflict instead of losing the story it went to.
func (s *Service) SetStatus(ctx context.Context, workspaceID, id uuid.UUID, status seodomain.Status) (seodomain.Opportunity, error) {
	o, err := s.repo.Get(ctx, workspaceID, id)
	if err != nil {
		return o, err
	}
	from := o.Status
	switch status {
	case seodomain.StatusDismissed:
		o.Dismiss(s.now())
	case seodomain.StatusOpen:
		o.Reopen(s.now())
	default:
		return o, apperror.New(apperror.ValidationFailed, "status must be open or dismissed")
	}
	return o, s.repo.Save(ctx, o, from)
}

// Next returns up to n open opportunities in language, best first.
func (s *Service) Next(ctx context.Context, workspaceID uuid.UUID, language string, n int) ([]seodomain.Opportunity, error) {
	if n <= 0 {
		return nil, nil
	}
	open, _, err := s.repo.List(ctx, workspaceID, Filter{Statuses: []seodomain.Status{seodomain.StatusOpen}, Language: language, Limit: n})
	return open, err
}

// Claim gives an open opportunity to a story and reports whether it did:
// false when it is no longer open (another run took it, or it was
// dismissed). Call it in the transaction that creates the story.
func (s *Service) Claim(ctx context.Context, workspaceID, opportunityID, storyID uuid.UUID) (bool, error) {
	o, err := s.repo.Get(ctx, workspaceID, opportunityID)
	if apperror.Is(err, apperror.NotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := o.Use(storyID, s.now()); apperror.Is(err, apperror.InvalidTransition) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	err = s.repo.Save(ctx, o, seodomain.StatusOpen)
	if apperror.Is(err, apperror.Conflict) {
		return false, nil
	}
	return err == nil, err
}

// Profile returns the workspace's SEO context.
func (s *Service) Profile(ctx context.Context, workspaceID uuid.UUID) (seodomain.Profile, error) {
	var p seodomain.Profile
	err := s.settings.Get(ctx, "seo:"+workspaceID.String(), &p)
	if apperror.Is(err, apperror.NotFound) {
		return seodomain.Profile{Country: "TR", Language: "tr"}, nil
	}
	return p, err
}

// SaveProfile stores the workspace's SEO context.
func (s *Service) SaveProfile(ctx context.Context, workspaceID, by uuid.UUID, p seodomain.Profile) (seodomain.Profile, error) {
	p.Domain = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(p.Domain), "https://"), "http://"), "/")
	p.Country = strings.ToUpper(strings.TrimSpace(p.Country))
	if p.Language != "tr" && p.Language != "en" {
		return p, apperror.New(apperror.ValidationFailed, "language must be tr or en")
	}
	if len(p.Country) != 2 {
		return p, apperror.New(apperror.ValidationFailed, "country must be a two-letter code such as TR")
	}
	if len(p.Topics) > 2000 {
		return p, apperror.New(apperror.ValidationFailed, "topics are too long")
	}
	p.ProjectID = strings.TrimSpace(p.ProjectID)
	p.Seeds = cleanList(p.Seeds, func(s string) string { return strings.Join(strings.Fields(s), " ") })
	p.Competitors = cleanList(p.Competitors, func(s string) string {
		return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "https://"), "http://"), "/")
	})
	switch {
	case len(p.Seeds) > seodomain.MaxSeeds:
		return p, apperror.New(apperror.ValidationFailed, fmt.Sprintf("at most %d seed keywords", seodomain.MaxSeeds))
	case len(p.Competitors) > seodomain.MaxCompetitors:
		return p, apperror.New(apperror.ValidationFailed, fmt.Sprintf("at most %d competitor domains", seodomain.MaxCompetitors))
	case p.MaxDifficulty < 0 || p.MaxDifficulty > 100:
		return p, apperror.New(apperror.ValidationFailed, "the difficulty ceiling must be 0–100")
	case p.MinVolume < 0:
		return p, apperror.New(apperror.ValidationFailed, "the volume floor must not be negative")
	}
	for _, c := range p.Competitors {
		if !strings.Contains(c, ".") || strings.ContainsAny(c, " /") {
			return p, apperror.New(apperror.ValidationFailed, "competitors are domains such as example.com: "+c)
		}
	}
	return p, s.settings.Put(ctx, "seo:"+workspaceID.String(), p, by, s.now())
}

// QueueRefresh queues a refresh from OpenSEO.
func (s *Service) QueueRefresh(ctx context.Context, workspaceID uuid.UUID, count int) (*jobdomain.Job, error) {
	p, err := s.Profile(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if s.direct(ctx) != nil {
		if p.ProjectID == "" {
			return nil, apperror.New(apperror.ValidationFailed, "choose the OpenSEO project in the SEO settings first")
		}
		if len(p.SeedList()) == 0 && len(p.Competitors) == 0 {
			return nil, apperror.New(apperror.ValidationFailed, "set seed keywords, topics or competitor domains first")
		}
	} else if p.Domain == "" && strings.TrimSpace(p.Topics) == "" {
		return nil, apperror.New(apperror.ValidationFailed, "set the site domain or topics first")
	}
	if count <= 0 || count > 50 {
		count = 20
	}
	payload, _ := json.Marshal(RefreshPayload{WorkspaceID: workspaceID, Count: count})
	j, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{WorkspaceID: workspaceID, Queue: jobdomain.QueueAgent, Kind: KindRefresh, Payload: payload, MaxAttempts: 1})
	return j, err
}

var refreshSchema = json.RawMessage(`{
  "type": "object",
  "required": ["opportunities"],
  "properties": {
    "opportunities": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["keyword", "volume", "difficulty", "intent", "note"],
        "properties": {
          "keyword": {"type": "string"},
          "volume": {"type": "integer", "description": "monthly searches, 0 if unknown"},
          "difficulty": {"type": "integer", "description": "0-100, -1 if unknown"},
          "intent": {"type": "string"},
          "note": {"type": "string", "description": "why this is worth writing for"}
        }
      }
    }
  }
}`)

// Refresh runs a seo.refresh job: directly against OpenSEO's API when a
// key is set, else through the agent and the host's OpenSEO plugin.
func (s *Service) Refresh(ctx context.Context, p RefreshPayload) error {
	prof, err := s.Profile(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	if r := s.direct(ctx); r != nil {
		return s.refreshDirect(ctx, r, prof, p)
	}
	known, err := s.repo.Keywords(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	known = known[:min(len(known), 200)]
	prompt := fmt.Sprintf(`Find %d keyword opportunities with the OpenSEO tools for content in %s for searchers in %s.
Site: %s
Topics: %s

Use OpenSEO's research tools (for example get_search_opportunities, get_domain_keyword_suggestions, research_keywords or get_keyword_metrics) with the site's project when one exists. Keep usage modest: well under 1000 credits.
Prefer informational questions people search for that an educational article or short video can answer well, with real demand and achievable difficulty. Skip brand terms and anything already covered:
%s

Return the keywords in the content language as JSON.`,
		p.Count, map[string]string{"tr": "Turkish", "en": "English"}[prof.Language], prof.Country,
		cmp.Or(prof.Domain, "(none)"), cmp.Or(prof.Topics, "(derive them from the site)"), strings.Join(known, "; "))
	res, err := s.agent.Run(ctx, claude.Request{
		Dir: s.workDir, Prompt: prompt, JSONSchema: refreshSchema, UserSettings: true,
		Allowed: []string{"mcp__plugin_openseo_openseo", "mcp__openseo"}, Timeout: 15 * time.Minute,
	})
	if err != nil {
		return jobdomain.Permanent(fmt.Errorf("OpenSEO refresh: %w (is the OpenSEO plugin connected in this host's Claude Code?)", err))
	}
	var out struct {
		Opportunities []struct {
			Keyword    string `json:"keyword"`
			Volume     int    `json:"volume"`
			Difficulty int    `json:"difficulty"`
			Intent     string `json:"intent"`
			Note       string `json:"note"`
		} `json:"opportunities"`
	}
	if err := json.Unmarshal(res.Structured, &out); err != nil {
		return jobdomain.Permanent(err)
	}
	items := make([]Item, 0, len(out.Opportunities))
	for _, o := range out.Opportunities {
		items = append(items, Item{Keyword: o.Keyword, Language: prof.Language, Volume: o.Volume, Difficulty: o.Difficulty, Intent: o.Intent, Note: o.Note})
	}
	_, err = s.Import(ctx, p.WorkspaceID, items, "openseo")
	return err
}

func cleanList(in []string, norm func(string) string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range in {
		v = norm(v)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

// refreshDirect researches the profile's seeds and competitors with
// OpenSEO and imports the best new keywords.
func (s *Service) refreshDirect(ctx context.Context, r Research, prof seodomain.Profile, p RefreshPayload) error {
	if prof.ProjectID == "" {
		return jobdomain.Permanent(errors.New("choose the OpenSEO project in the SEO settings"))
	}
	market := openseo.Market{LocationCode: openseo.Locations[prof.Country], LanguageCode: prof.Language}
	var (
		cands    []seodomain.Candidate
		failures []string
	)
	if seeds := prof.SeedList(); len(seeds) > 0 {
		results, err := r.ResearchKeywords(ctx, prof.ProjectID, seeds, market)
		if err != nil {
			return jobdomain.Permanent(err)
		}
		for _, res := range results {
			if !res.OK {
				failures = append(failures, fmt.Sprintf("%q: %s", res.Seed, res.Error))
				continue
			}
			cands = append(cands, candidates(res.Rows, "seed “"+res.Seed+"”", true)...)
		}
	}
	for _, domain := range prof.Competitors {
		rows, err := r.DomainKeywords(ctx, prof.ProjectID, domain, market)
		if err != nil {
			failures = append(failures, domain+": "+err.Error())
			continue
		}
		cands = append(cands, candidates(rows, domain, false)...)
	}
	if len(cands) == 0 && len(failures) > 0 {
		return jobdomain.Permanent(errors.New("OpenSEO research failed: " + strings.Join(failures, "; ")))
	}
	known, err := s.repo.Keywords(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	brand, _, _ := strings.Cut(prof.Domain, ".")
	terms := append(prof.SeedList(), strings.FieldsFunc(prof.Topics, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })...)
	picks := seodomain.Pick(cands, known, brand, terms, p.Count, prof.Difficulty(), prof.MinVolume)
	items := make([]Item, 0, len(picks))
	for _, c := range picks {
		items = append(items, Item{Keyword: c.Keyword, Language: prof.Language, Volume: max(c.Volume, 0), Difficulty: c.Difficulty,
			Intent: c.Intent, Note: note(c)})
	}
	_, err = s.Import(ctx, p.WorkspaceID, items, "openseo")
	return err
}

func candidates(rows []openseo.Keyword, from string, seeded bool) []seodomain.Candidate {
	out := make([]seodomain.Candidate, 0, len(rows))
	for _, k := range rows {
		c := seodomain.Candidate{Keyword: k.Keyword, Volume: -1, Difficulty: -1, Intent: k.Intent, From: from, Seeded: seeded}
		if k.SearchVolume != nil {
			c.Volume = *k.SearchVolume
		}
		if k.KeywordDifficulty != nil {
			c.Difficulty = *k.KeywordDifficulty
		}
		out = append(out, c)
	}
	return out
}

func note(c seodomain.Candidate) string {
	parts := []string{"From " + c.From}
	if c.Volume >= 0 {
		parts = append(parts, fmt.Sprintf("%d searches/month", c.Volume))
	}
	if c.Difficulty >= 0 {
		parts = append(parts, fmt.Sprintf("difficulty %d", c.Difficulty))
	}
	return strings.Join(parts, " · ")
}

// OpenSEOStatus is the instance's OpenSEO connection as the SEO
// settings see it.
type OpenSEOStatus struct {
	Configured bool // an API key is set
	Account    *openseo.Account
	Projects   []openseo.Project
	Error      string
}

// OpenSEO checks the API key and lists its projects; both calls are free.
func (s *Service) OpenSEO(ctx context.Context) OpenSEOStatus {
	r := s.direct(ctx)
	if r == nil {
		return OpenSEOStatus{}
	}
	st := OpenSEOStatus{Configured: true}
	a, err := r.Whoami(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Account = &a
	if st.Projects, err = r.Projects(ctx); err != nil {
		st.Error = err.Error()
	}
	return st
}
