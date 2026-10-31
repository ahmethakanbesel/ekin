// Package handler implements the site operations of
// gen.StrictServerInterface.
package handler

import (
	"context"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	sitesvc "github.com/ahmethakanbesel/ekin/internal/service/site/v1"
	"github.com/ahmethakanbesel/ekin/internal/transport/http/principal"
)

// Handler serves the workspace's site.
type Handler struct{ svc *sitesvc.Service }

// New returns a Handler.
func New(svc *sitesvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) GetSite(ctx context.Context, req gen.GetSiteRequestObject) (gen.GetSiteResponseObject, error) {
	o, err := h.svc.Overview(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetSite200JSONResponse(overviewToWire(o)), nil
}

func (h *Handler) SaveSite(ctx context.Context, req gen.SaveSiteRequestObject) (gen.SaveSiteResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	st := sitedomain.Settings{URL: req.Body.Url}
	if req.Body.MaxPages != nil {
		st.MaxPages = *req.Body.MaxPages
	}
	if req.Body.Schedule != nil {
		st.Schedule = sitedomain.Schedule(*req.Body.Schedule)
	}
	if _, err := h.svc.SaveSite(ctx, req.WorkspaceId, p.User.ID(), st); err != nil {
		return nil, err
	}
	o, err := h.svc.Overview(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.SaveSite200JSONResponse(overviewToWire(o)), nil
}

func (h *Handler) ListSiteCrawls(ctx context.Context, req gen.ListSiteCrawlsRequestObject) (gen.ListSiteCrawlsResponseObject, error) {
	limit := 20
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	list, err := h.svc.Crawls(ctx, req.WorkspaceId, limit)
	if err != nil {
		return nil, err
	}
	out := gen.ListSiteCrawls200JSONResponse{Crawls: make([]gen.SiteCrawl, 0, len(list))}
	for _, c := range list {
		out.Crawls = append(out.Crawls, crawlToWire(c))
	}
	return out, nil
}

func (h *Handler) CrawlSite(ctx context.Context, req gen.CrawlSiteRequestObject) (gen.CrawlSiteResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	c, err := h.svc.CrawlNow(ctx, req.WorkspaceId, p.User.ID())
	if err != nil {
		return nil, err
	}
	return gen.CrawlSite200JSONResponse(crawlToWire(c)), nil
}

func (h *Handler) ListSitePages(ctx context.Context, req gen.ListSitePagesRequestObject) (gen.ListSitePagesResponseObject, error) {
	var f sitesvc.PageFilter
	if c := req.Params.Class; c != nil {
		if !c.Valid() {
			return nil, apperror.New(apperror.ValidationFailed, "class must be ok, redirect or error")
		}
		f.Class = sitedomain.StatusClass(*c)
	}
	if req.Params.Q != nil {
		f.Query = *req.Params.Q
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		f.After = *req.Params.Cursor
	}
	rows, next, err := h.svc.Pages(ctx, req.WorkspaceId, f)
	if err != nil {
		return nil, err
	}
	out := gen.ListSitePages200JSONResponse{Pages: make([]gen.SitePage, 0, len(rows))}
	for _, r := range rows {
		out.Pages = append(out.Pages, pageToWire(r))
	}
	if next != "" {
		out.NextCursor = &next
	}
	return out, nil
}

func (h *Handler) ListSiteFindings(ctx context.Context, req gen.ListSiteFindingsRequestObject) (gen.ListSiteFindingsResponseObject, error) {
	var f sitesvc.FindingFilter
	if req.Params.Status != nil {
		for _, raw := range *req.Params.Status {
			st, err := sitedomain.ParseFindingStatus(string(raw))
			if err != nil {
				return nil, err
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	if req.Params.Rule != nil {
		f.Rule = sitedomain.Rule(*req.Params.Rule)
	}
	if req.Params.Severity != nil {
		sv, err := sitedomain.ParseSeverity(string(*req.Params.Severity))
		if err != nil {
			return nil, err
		}
		f.Severity = sv
	}
	if req.Params.Url != nil {
		f.URL = *req.Params.Url
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		f.After = *req.Params.Cursor
	}
	list, next, err := h.svc.Findings(ctx, req.WorkspaceId, f)
	if err != nil {
		return nil, err
	}
	out := gen.ListSiteFindings200JSONResponse{Findings: make([]gen.SiteFinding, 0, len(list))}
	for _, x := range list {
		out.Findings = append(out.Findings, findingToWire(x))
	}
	if next != "" {
		out.NextCursor = &next
	}
	return out, nil
}

func (h *Handler) UpdateSiteFinding(ctx context.Context, req gen.UpdateSiteFindingRequestObject) (gen.UpdateSiteFindingResponseObject, error) {
	to := sitedomain.FindingStatus(req.Body.Status)
	if to != sitedomain.FindingOpen && to != sitedomain.FindingIgnored {
		return nil, apperror.New(apperror.ValidationFailed, "status must be open or ignored; crawls resolve findings")
	}
	f, err := h.svc.SetFindingStatus(ctx, req.WorkspaceId, req.FindingId, to)
	if err != nil {
		return nil, err
	}
	return gen.UpdateSiteFinding200JSONResponse(findingToWire(f)), nil
}

func overviewToWire(o sitesvc.Overview) gen.SiteOverview {
	out := gen.SiteOverview{Open: make([]gen.SiteRuleCount, 0, len(o.Open))}
	if s := o.Site; s != nil {
		out.Site = &gen.Site{Url: s.URL(), MaxPages: s.MaxPages(), Schedule: gen.SiteSchedule(s.Schedule()), UpdatedAt: s.UpdatedAt(),
			NextCrawlAt: timePtr(s.NextCrawlAt())}
	}
	if o.Latest != nil {
		c := crawlToWire(o.Latest)
		out.LatestCrawl = &c
	}
	if o.LastDone != nil {
		c := crawlToWire(o.LastDone)
		out.LastDoneCrawl = &c
	}
	for _, rc := range o.Open {
		out.Open = append(out.Open, gen.SiteRuleCount{Rule: string(rc.Rule), Severity: gen.SiteSeverity(rc.Rule.Severity()), Count: rc.Count})
	}
	return out
}

func crawlToWire(c *sitedomain.Crawl) gen.SiteCrawl {
	out := gen.SiteCrawl{Id: c.ID(), Url: c.URL(), Status: gen.SiteCrawlStatus(c.Status()), Trigger: gen.SiteCrawlTrigger(c.Trigger()),
		Pages: c.Pages(), CreatedAt: c.CreatedAt(), StartedAt: timePtr(c.StartedAt()), FinishedAt: timePtr(c.FinishedAt())}
	if c.Status() == sitedomain.CrawlDone {
		f := c.Facts()
		out.Facts = &gen.SiteFacts{RobotsFound: f.RobotsFound, Sitemaps: nonNil(f.Sitemaps), SitemapUrls: f.SitemapURLs,
			AiBlocked: nonNil(f.AIBlocked), LlmsTxt: f.LLMsTxt, Disallowed: f.Disallowed, Truncated: f.Truncated}
	}
	if c.Failure() != "" {
		failure := c.Failure()
		out.Failure = &failure
	}
	return out
}

func pageToWire(r sitesvc.PageRow) gen.SitePage {
	p := r.Page
	return gen.SitePage{Url: p.URL, Status: p.Status, Class: gen.SitePageClass(p.Class()), Error: strPtr(p.Error),
		RedirectTo: strPtr(p.RedirectTo), ContentType: strPtr(p.ContentType), Title: strPtr(p.Title), Description: strPtr(p.Description),
		Canonical: strPtr(p.Canonical), Lang: strPtr(p.Lang), H1: nonNil(p.H1), Noindex: p.Noindex, Words: p.Words, Types: nonNil(p.Types),
		Links: len(p.Links), InSitemap: p.InSitemap, Depth: p.Depth, OpenFindings: r.OpenFindings, FirstSeen: p.FirstSeen, LastSeen: p.LastSeen}
}

func findingToWire(f *sitedomain.Finding) gen.SiteFinding {
	return gen.SiteFinding{Id: f.ID(), Rule: string(f.Rule()), Severity: gen.SiteSeverity(f.Severity()), Url: f.URL(),
		Evidence: nonNil(f.Evidence()), Status: gen.SiteFindingStatus(f.Status()), FirstSeen: f.FirstSeen(), LastSeen: f.LastSeen(),
		ResolvedAt: timePtr(f.ResolvedAt())}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
