// Package site runs the site use cases: setting the site up, crawling it
// (on setup, on demand and on its schedule), auditing what the crawl
// found, and listing pages and findings.
package site

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/adapter/crawl"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	"github.com/ahmethakanbesel/ekin/internal/db"
	jobdomain "github.com/ahmethakanbesel/ekin/internal/domain/job/v1"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	siterepo "github.com/ahmethakanbesel/ekin/internal/repository/site/v1"
	"github.com/ahmethakanbesel/ekin/pkg/id"
)

// KindCrawl is the job that crawls a site and audits it.
const KindCrawl = "site.crawl"

// CrawlPayload is the site.crawl job payload.
type CrawlPayload struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	CrawlID     uuid.UUID `json:"crawlId"`
}

// A crawl that has not finished after this long stopped without saying
// so (its worker went away); a new crawl may start over it.
const (
	staleQueued  = 6 * time.Hour
	staleRunning = 3 * time.Hour
)

// Crawler fetches a site (internal/adapter/crawl).
type Crawler interface {
	Crawl(ctx context.Context, start string, o crawl.Options) (crawl.Result, error)
	// Sitemap lists a site's pages from its sitemaps alone.
	Sitemap(ctx context.Context, site string, delay time.Duration) (crawl.SitemapResult, error)
	// Titles reads the titles of pages on site robots.txt allows.
	Titles(ctx context.Context, site string, urls []string, delay time.Duration) (map[string]string, error)
}

// Enqueuer is the part of the job service this package needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, spec jobdomain.Spec) (*jobdomain.Job, bool, error)
}

// Config tunes crawling.
type Config struct {
	// Delay is the pause between requests to a site.
	Delay time.Duration
}

// Service runs site use cases.
type Service struct {
	repo    siterepo.Repository
	tx      db.TxManager
	jobs    Enqueuer
	crawler Crawler
	cfg     Config
	log     *slog.Logger
	now     func() time.Time
}

// NewService returns a Service.
func NewService(repo siterepo.Repository, tx db.TxManager, jobs Enqueuer, crawler Crawler, cfg Config, log *slog.Logger) *Service {
	return &Service{repo: repo, tx: tx, jobs: jobs, crawler: crawler, cfg: cfg, log: log, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Overview is the site as its page shows it.
type Overview struct {
	Site *sitedomain.Site // nil when none is set up
	// Latest is the newest crawl, whatever its status; LastDone the
	// newest that went through (they are the same when it did).
	Latest, LastDone *sitedomain.Crawl
	// Open counts the open findings per rule, most severe first.
	Open []siterepo.RuleCount
}

// Overview returns the workspace's site, its latest crawls and its open
// findings per rule.
func (s *Service) Overview(ctx context.Context, ws uuid.UUID) (Overview, error) {
	var o Overview
	site, err := s.repo.Site(ctx, ws)
	if apperror.Is(err, apperror.NotFound) {
		return o, nil
	}
	if err != nil {
		return o, err
	}
	o.Site = site
	crawls, err := s.repo.Crawls(ctx, ws, 20)
	if err != nil {
		return o, err
	}
	for _, c := range crawls {
		if o.Latest == nil {
			o.Latest = c
		}
		if c.Status() == sitedomain.CrawlDone {
			o.LastDone = c
			break
		}
	}
	o.Open, err = s.repo.RuleCounts(ctx, ws, sitedomain.FindingOpen)
	return o, err
}

// SaveSite sets the site up or changes it. A new site, or one moved to
// another address, is crawled right away; a moved site's old pages and
// findings are dropped.
func (s *Service) SaveSite(ctx context.Context, ws, by uuid.UUID, st sitedomain.Settings) (*sitedomain.Site, error) {
	var out *sitedomain.Site
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		now := s.now()
		site, err := s.repo.Site(ctx, ws)
		crawlNow := false
		switch {
		case apperror.Is(err, apperror.NotFound):
			if site, err = sitedomain.New(ws, st, by, now); err != nil {
				return err
			}
			crawlNow = true
		case err != nil:
			return err
		default:
			moved, err := site.Update(st, by, now)
			if err != nil {
				return err
			}
			if moved {
				if err := s.repo.ClearInventory(ctx, ws); err != nil {
					return err
				}
				crawlNow = true
			}
		}
		if err := s.repo.PutSite(ctx, site); err != nil {
			return err
		}
		out = site
		if crawlNow {
			_, err = s.start(ctx, site, sitedomain.TriggerSetup, by)
		}
		return err
	})
	return out, err
}

// CrawlNow starts a crawl of the workspace's site on by's behalf.
func (s *Service) CrawlNow(ctx context.Context, ws, by uuid.UUID) (*sitedomain.Crawl, error) {
	var out *sitedomain.Crawl
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		site, err := s.repo.Site(ctx, ws)
		if apperror.Is(err, apperror.NotFound) {
			return apperror.New(apperror.ValidationFailed, "set up the site in workspace settings first")
		}
		if err != nil {
			return err
		}
		out, err = s.start(ctx, site, sitedomain.TriggerManual, by)
		return err
	})
	return out, err
}

// TriggerDue starts the scheduled crawls that are due and returns how
// many it started. A site still being crawled waits for its next turn.
func (s *Service) TriggerDue(ctx context.Context) (int, error) {
	due, err := s.repo.DueSites(ctx, s.now())
	if err != nil {
		return 0, err
	}
	started := 0
	for _, site := range due {
		err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
			_, err := s.start(ctx, site, sitedomain.TriggerSchedule, uuid.Nil)
			if apperror.Is(err, apperror.Conflict) {
				site.Crawled(s.now())
				return s.repo.PutSite(ctx, site)
			}
			if err == nil {
				started++
			}
			return err
		})
		if err != nil {
			s.log.WarnContext(ctx, "cannot start a scheduled crawl", slog.String("workspace", site.WorkspaceID().String()), slog.Any("error", err))
		}
	}
	n, err := s.triggerCompetitors(ctx)
	return started + n, err
}

// start queues a crawl of site unless one is under way. A setup crawl
// (the site is new or moved) replaces one under way instead. Call it
// inside a transaction.
func (s *Service) start(ctx context.Context, site *sitedomain.Site, trigger sitedomain.Trigger, by uuid.UUID) (*sitedomain.Crawl, error) {
	now := s.now()
	ws := site.WorkspaceID()
	latest, err := s.repo.Crawls(ctx, ws, 1)
	if err != nil {
		return nil, err
	}
	if len(latest) == 1 && latest[0].Active() {
		c := latest[0]
		reason := "the crawl stopped without finishing"
		switch {
		case trigger == sitedomain.TriggerSetup:
			reason = "the site's address changed during the crawl"
		case !stale(c, now):
			return nil, apperror.New(apperror.Conflict, "the site is being crawled; wait for this crawl to finish")
		}
		if err := c.Fail(reason, now); err != nil {
			return nil, err
		}
		if err := s.repo.SaveCrawl(ctx, c); err != nil {
			return nil, err
		}
	}
	c, err := sitedomain.NewCrawl(id.New(), ws, site.URL(), trigger, by, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateCrawl(ctx, c); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(CrawlPayload{WorkspaceID: ws, CrawlID: c.ID()})
	if _, _, err := s.jobs.Enqueue(ctx, jobdomain.Spec{
		WorkspaceID: ws, Queue: jobdomain.QueueIO, Kind: KindCrawl, Payload: payload, MaxAttempts: 2,
		UniqueKey: KindCrawl + ":" + c.ID().String(), ConcurrencyKey: "site:" + ws.String(),
	}); err != nil {
		return nil, err
	}
	site.Crawled(now)
	return c, s.repo.PutSite(ctx, site)
}

func stale(c *sitedomain.Crawl, now time.Time) bool {
	if c.Status() == sitedomain.CrawlRunning {
		return now.Sub(c.StartedAt()) > staleRunning
	}
	return now.Sub(c.CreatedAt()) > staleQueued
}

// RunCrawl runs a site.crawl job: it crawls the site, audits what it
// found and records the result. A crawl that cannot go through is
// recorded as failed; only a cancelled job returns an error.
func (s *Service) RunCrawl(ctx context.Context, p CrawlPayload) error {
	c, err := s.repo.Crawl(ctx, p.WorkspaceID, p.CrawlID)
	if apperror.Is(err, apperror.NotFound) {
		return jobdomain.Permanent(err)
	}
	if err != nil {
		return err
	}
	if !c.Active() {
		return nil
	}
	site, err := s.repo.Site(ctx, p.WorkspaceID)
	if err != nil {
		return s.failCrawl(ctx, c, "the site is no longer set up")
	}
	if site.URL() != c.URL() {
		return s.failCrawl(ctx, c, "the site moved to another address before the crawl started")
	}
	if err := c.Start(s.now()); err != nil {
		return jobdomain.Permanent(err)
	}
	if err := s.repo.SaveCrawl(ctx, c); err != nil {
		return err
	}
	crawlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	superseded := false
	progress := func(n int) {
		ok, err := s.progress(ctx, c.WorkspaceID(), c.ID(), n)
		if err != nil && ctx.Err() == nil {
			s.log.WarnContext(ctx, "cannot record crawl progress", slog.Any("error", err))
		}
		if !ok {
			superseded = true
			cancel()
		}
	}
	res, err := s.crawler.Crawl(crawlCtx, c.URL(), crawl.Options{MaxPages: site.MaxPages(), Delay: s.cfg.Delay, Progress: progress})
	if superseded {
		return nil
	}
	if err != nil {
		if ctx.Err() != nil {
			// Cancelled or the worker is stopping: the job ends and a
			// retry starts the crawl over.
			return ctx.Err()
		}
		return s.failCrawl(ctx, c, crawlFailure(err))
	}
	return s.finish(ctx, c, res)
}

// progress records that the crawl fetched n pages and reports whether it
// is still under way (false: it was replaced, and should stop).
func (s *Service) progress(ctx context.Context, ws, crawlID uuid.UUID, n int) (bool, error) {
	active := true
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.repo.Crawl(ctx, ws, crawlID)
		if err != nil {
			return err
		}
		if active = cur.Active(); !active {
			return nil
		}
		cur.Progress(n)
		return s.repo.SaveCrawl(ctx, cur)
	})
	return active, err
}

func crawlFailure(err error) string {
	if errors.Is(err, crawl.ErrPrivateAddress) {
		return "the site's address points to an internal network address, which Ekin does not crawl: " + err.Error()
	}
	return err.Error()
}

func (s *Service) failCrawl(ctx context.Context, c *sitedomain.Crawl, reason string) error {
	if err := c.Fail(reason, s.now()); err != nil {
		return jobdomain.Permanent(err)
	}
	return s.repo.SaveCrawl(context.WithoutCancel(ctx), c)
}

// finish turns the crawl's result into the inventory, reconciles the
// findings with the audit and marks the crawl done, in one transaction.
func (s *Service) finish(ctx context.Context, c *sitedomain.Crawl, res crawl.Result) error {
	now := s.now()
	ws := c.WorkspaceID()
	root := c.URL()
	if base, err := url.Parse(c.URL()); err == nil {
		if n, ok := crawl.Normalize(base, c.URL()); ok {
			root = n
		}
	}
	facts := sitedomain.Facts{RobotsFound: res.RobotsFound, Sitemaps: res.Sitemaps, SitemapURLs: res.SitemapURLs,
		AIBlocked: res.AIBlocked, LLMsTxt: res.LLMsTxt, Disallowed: res.Disallowed, Truncated: res.Truncated}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// A crawl replaced meanwhile (the site moved) leaves no trace.
		if cur, err := s.repo.Crawl(ctx, ws, c.ID()); err != nil || !cur.Active() {
			return err
		}
		first, err := s.repo.FirstSeen(ctx, ws)
		if err != nil {
			return err
		}
		pages := make([]sitedomain.Page, 0, len(res.Pages))
		for _, p := range res.Pages {
			seen, ok := first[p.URL]
			if !ok {
				seen = now
			}
			pages = append(pages, sitedomain.Page{URL: p.URL, Status: p.Status, Error: p.Error, RedirectTo: p.RedirectTo,
				ContentType: p.ContentType, Title: p.Title, Description: p.Description, Canonical: p.Canonical, Lang: p.Lang,
				H1: p.H1, Noindex: p.Noindex, Words: p.Words, Types: p.Types, Links: p.Links, InSitemap: p.InSitemap,
				Depth: p.Depth, FirstSeen: seen, LastSeen: now})
		}
		if err := s.repo.ReplacePages(ctx, ws, pages); err != nil {
			return err
		}
		existing, err := s.repo.AllFindings(ctx, ws)
		if err != nil {
			return err
		}
		raised := sitedomain.Audit(sitedomain.Inventory{Root: root, Pages: pages, Facts: facts})
		if err := s.repo.PutFindings(ctx, sitedomain.Reconcile(existing, raised, ws, c.ID(), id.New, now)); err != nil {
			return err
		}
		if err := c.Finish(facts, len(pages), now); err != nil {
			return jobdomain.Permanent(err)
		}
		return s.repo.SaveCrawl(ctx, c)
	})
}

// Crawls returns the workspace's latest crawls, newest first.
func (s *Service) Crawls(ctx context.Context, ws uuid.UUID, limit int) ([]*sitedomain.Crawl, error) {
	return s.repo.Crawls(ctx, ws, limit)
}

// PageFilter narrows a listing of pages.
type PageFilter = siterepo.PageFilter

// PageRow is a page and its open findings count.
type PageRow = siterepo.PageRow

// Pages returns a page of the latest crawl's pages, by address.
func (s *Service) Pages(ctx context.Context, ws uuid.UUID, f PageFilter) ([]PageRow, string, error) {
	return s.repo.Pages(ctx, ws, f)
}

// FindingFilter narrows a listing of findings.
type FindingFilter = siterepo.FindingFilter

// Findings returns a page of findings, most severe first.
func (s *Service) Findings(ctx context.Context, ws uuid.UUID, f FindingFilter) ([]*sitedomain.Finding, string, error) {
	return s.repo.Findings(ctx, ws, f)
}

// SetFindingStatus ignores an open finding or brings an ignored one back.
func (s *Service) SetFindingStatus(ctx context.Context, ws, findingID uuid.UUID, to sitedomain.FindingStatus) (*sitedomain.Finding, error) {
	var out *sitedomain.Finding
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		f, err := s.repo.Finding(ctx, ws, findingID)
		if err != nil {
			return err
		}
		if err := f.SetStatus(to, s.now()); err != nil {
			return err
		}
		out = f
		return s.repo.PutFindings(ctx, []*sitedomain.Finding{f})
	})
	return out, err
}
