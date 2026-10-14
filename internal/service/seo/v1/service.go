// Package seo manages the keyword-opportunity pool: import, refresh from
// OpenSEO through the agent, and hand opportunities to pipelines.
package seo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/adapter/claude"
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

// Service runs SEO use cases.
type Service struct {
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

// List returns the pool, best open opportunities first.
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID, status seodomain.Status) ([]seodomain.Opportunity, error) {
	list, err := s.repo.List(ctx, workspaceID, status)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(list, func(a, b seodomain.Opportunity) int {
		if a.Status != b.Status {
			return cmp.Compare(rank(a.Status), rank(b.Status))
		}
		return cmp.Compare(b.Score(), a.Score())
	})
	return list, nil
}

func rank(s seodomain.Status) int {
	return map[seodomain.Status]int{seodomain.StatusOpen: 0, seodomain.StatusUsed: 1, seodomain.StatusDismissed: 2}[s]
}

// SetStatus dismisses or reopens an opportunity.
func (s *Service) SetStatus(ctx context.Context, workspaceID, id uuid.UUID, status seodomain.Status) (seodomain.Opportunity, error) {
	o, err := s.repo.Get(ctx, workspaceID, id)
	if err != nil {
		return o, err
	}
	switch status {
	case seodomain.StatusDismissed:
		o.Dismiss(s.now())
	case seodomain.StatusOpen:
		o.Reopen(s.now())
	default:
		return o, apperror.New(apperror.ValidationFailed, "status must be open or dismissed")
	}
	return o, s.repo.Save(ctx, o)
}

// Next returns up to n open opportunities in language, best first.
func (s *Service) Next(ctx context.Context, workspaceID uuid.UUID, language string, n int) ([]seodomain.Opportunity, error) {
	open, err := s.List(ctx, workspaceID, seodomain.StatusOpen)
	if err != nil {
		return nil, err
	}
	var out []seodomain.Opportunity
	for _, o := range open {
		if o.Language == language {
			out = append(out, o)
			if len(out) == n {
				break
			}
		}
	}
	return out, nil
}

// MarkUsed records that a story took the opportunity.
func (s *Service) MarkUsed(ctx context.Context, workspaceID, opportunityID, storyID uuid.UUID) error {
	o, err := s.repo.Get(ctx, workspaceID, opportunityID)
	if err != nil {
		return err
	}
	if err := o.Use(storyID, s.now()); err != nil {
		return err
	}
	return s.repo.Save(ctx, o)
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
	return p, s.settings.Put(ctx, "seo:"+workspaceID.String(), p, by, s.now())
}

// QueueRefresh queues a refresh from OpenSEO.
func (s *Service) QueueRefresh(ctx context.Context, workspaceID uuid.UUID, count int) (*jobdomain.Job, error) {
	p, err := s.Profile(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if p.Domain == "" && strings.TrimSpace(p.Topics) == "" {
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

// Refresh runs a seo.refresh job: the agent queries OpenSEO with the
// host's Claude Code OpenSEO connection and returns structured keywords.
func (s *Service) Refresh(ctx context.Context, p RefreshPayload) error {
	prof, err := s.Profile(ctx, p.WorkspaceID)
	if err != nil {
		return err
	}
	existing, err := s.repo.List(ctx, p.WorkspaceID, "")
	if err != nil {
		return err
	}
	var known []string
	for i, o := range existing {
		if i == 200 {
			break
		}
		known = append(known, o.Keyword)
	}
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
