package site

import (
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/pkg/timeutil"
)

// CrawlStatus is where a crawl is.
//
//	queued → running → done | failed; queued → failed
type CrawlStatus string

const (
	CrawlQueued  CrawlStatus = "queued"
	CrawlRunning CrawlStatus = "running"
	CrawlDone    CrawlStatus = "done"
	CrawlFailed  CrawlStatus = "failed"
)

// Trigger is why a crawl started.
type Trigger string

const (
	TriggerManual   Trigger = "manual"
	TriggerSchedule Trigger = "schedule"
	TriggerSetup    Trigger = "setup" // the site was just set up or moved
)

// Facts are what a crawl learnt about the site as a whole.
type Facts struct {
	RobotsFound bool     `json:"robotsFound"`
	Sitemaps    []string `json:"sitemaps"`    // sitemap files read
	SitemapURLs int      `json:"sitemapUrls"` // page URLs they list on the site
	AIBlocked   []string `json:"aiBlocked"`   // AI crawlers robots.txt keeps off the start page
	LLMsTxt     bool     `json:"llmsTxt"`
	Disallowed  int      `json:"disallowed"` // URLs robots.txt kept Ekin off
	Truncated   bool     `json:"truncated"`  // stopped at the page limit
}

// Crawl is one pass over the site.
type Crawl struct {
	id          uuid.UUID
	workspaceID uuid.UUID
	url         string
	status      CrawlStatus
	trigger     Trigger
	requestedBy uuid.UUID // nil for scheduled crawls
	pages       int
	facts       Facts
	failure     string
	createdAt   time.Time
	startedAt   time.Time
	finishedAt  time.Time
}

// NewCrawl queues a crawl of url.
func NewCrawl(id, ws uuid.UUID, url string, trigger Trigger, by uuid.UUID, now time.Time) (*Crawl, error) {
	if id == uuid.Nil || ws == uuid.Nil {
		return nil, apperror.New(apperror.ValidationFailed, "crawl and workspace ids are required")
	}
	if _, err := ParseURL(url); err != nil {
		return nil, err
	}
	switch trigger {
	case TriggerManual, TriggerSchedule, TriggerSetup:
	default:
		return nil, apperror.New(apperror.ValidationFailed, "unknown crawl trigger")
	}
	return &Crawl{id: id, workspaceID: ws, url: url, status: CrawlQueued, trigger: trigger, requestedBy: by,
		createdAt: timeutil.Normalize(now)}, nil
}

// Active reports whether the crawl has not finished.
func (c *Crawl) Active() bool { return c.status == CrawlQueued || c.status == CrawlRunning }

// Start marks the crawl running. A running crawl may start again: its
// worker stopped and the job's retry runs it from the beginning.
func (c *Crawl) Start(now time.Time) error {
	if !c.Active() {
		return apperror.New(apperror.InvalidTransition, "the crawl is "+string(c.status))
	}
	c.status, c.pages, c.startedAt = CrawlRunning, 0, timeutil.Normalize(now)
	return nil
}

// Progress records how many pages the running crawl has fetched.
func (c *Crawl) Progress(pages int) {
	if c.status == CrawlRunning && pages > c.pages {
		c.pages = pages
	}
}

// Finish records a crawl that went through.
func (c *Crawl) Finish(f Facts, pages int, now time.Time) error {
	if c.status != CrawlRunning {
		return apperror.New(apperror.InvalidTransition, "the crawl is "+string(c.status))
	}
	c.status, c.facts, c.pages, c.finishedAt = CrawlDone, f, pages, timeutil.Normalize(now)
	return nil
}

// Fail records why the crawl could not go through.
func (c *Crawl) Fail(reason string, now time.Time) error {
	if !c.Active() {
		return apperror.New(apperror.InvalidTransition, "the crawl is "+string(c.status))
	}
	if len(reason) > 2000 {
		reason = reason[:2000]
	}
	c.status, c.failure, c.finishedAt = CrawlFailed, reason, timeutil.Normalize(now)
	return nil
}

func (c *Crawl) ID() uuid.UUID          { return c.id }
func (c *Crawl) WorkspaceID() uuid.UUID { return c.workspaceID }
func (c *Crawl) URL() string            { return c.url }
func (c *Crawl) Status() CrawlStatus    { return c.status }
func (c *Crawl) Trigger() Trigger       { return c.trigger }
func (c *Crawl) RequestedBy() uuid.UUID { return c.requestedBy }
func (c *Crawl) Pages() int             { return c.pages }
func (c *Crawl) Facts() Facts           { return c.facts }
func (c *Crawl) Failure() string        { return c.failure }
func (c *Crawl) CreatedAt() time.Time   { return c.createdAt }
func (c *Crawl) StartedAt() time.Time   { return c.startedAt }
func (c *Crawl) FinishedAt() time.Time  { return c.finishedAt }

// CrawlSnapshot is a Crawl's stored form.
type CrawlSnapshot struct {
	ID, WorkspaceID, RequestedBy     uuid.UUID
	URL, Status, Trigger, Failure    string
	Pages                            int
	Facts                            Facts
	CreatedAt, StartedAt, FinishedAt time.Time
}

func (c *Crawl) Snapshot() CrawlSnapshot {
	return CrawlSnapshot{ID: c.id, WorkspaceID: c.workspaceID, RequestedBy: c.requestedBy, URL: c.url, Status: string(c.status),
		Trigger: string(c.trigger), Failure: c.failure, Pages: c.pages, Facts: c.facts,
		CreatedAt: c.createdAt, StartedAt: c.startedAt, FinishedAt: c.finishedAt}
}

// RehydrateCrawl rebuilds a stored Crawl.
func RehydrateCrawl(sn CrawlSnapshot) (*Crawl, error) {
	switch CrawlStatus(sn.Status) {
	case CrawlQueued, CrawlRunning, CrawlDone, CrawlFailed:
	default:
		return nil, apperror.New(apperror.InternalError, "unknown crawl status")
	}
	c, err := NewCrawl(sn.ID, sn.WorkspaceID, sn.URL, Trigger(sn.Trigger), sn.RequestedBy, sn.CreatedAt)
	if err != nil {
		return nil, apperror.Wrap(apperror.InternalError, err, "stored crawl is invalid")
	}
	c.status, c.pages, c.facts, c.failure = CrawlStatus(sn.Status), sn.Pages, sn.Facts, sn.Failure
	c.startedAt, c.finishedAt = timeutil.Normalize(sn.StartedAt), timeutil.Normalize(sn.FinishedAt)
	return c, nil
}
