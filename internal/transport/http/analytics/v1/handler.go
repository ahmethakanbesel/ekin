// Package handler implements the Search Console operations of
// gen.StrictServerInterface.
package handler

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	analyticsdomain "github.com/ahmethakanbesel/ekin/internal/domain/analytics/v1"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	analyticssvc "github.com/ahmethakanbesel/ekin/internal/service/analytics/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	"github.com/ahmethakanbesel/ekin/internal/transport/http/principal"
)

// Handler serves Search Console.
type Handler struct {
	svc     *analyticssvc.Service
	stories *productionsvc.Service
}

// New returns a Handler.
func New(svc *analyticssvc.Service, stories *productionsvc.Service) *Handler {
	return &Handler{svc: svc, stories: stories}
}

func (h *Handler) status(ctx context.Context, ws uuid.UUID) (gen.SearchConsole, error) {
	out := gen.SearchConsole{Configured: h.svc.Configured(ctx)}
	c, err := h.svc.Status(ctx, ws)
	if err != nil || c == nil {
		return out, err
	}
	out.Connected = true
	out.Property = strPtr(c.Property())
	out.ConnectedAt = timePtr(c.ConnectedAt())
	out.LastSyncAt = timePtr(c.LastSyncAt())
	out.LastError = strPtr(c.LastError())
	if !c.SyncedThrough().IsZero() {
		d := openapi_types.Date{Time: c.SyncedThrough()}
		out.SyncedThrough = &d
	}
	return out, nil
}

func (h *Handler) GetSearchConsole(ctx context.Context, req gen.GetSearchConsoleRequestObject) (gen.GetSearchConsoleResponseObject, error) {
	s, err := h.status(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetSearchConsole200JSONResponse(s), nil
}

func (h *Handler) ConnectSearchConsole(ctx context.Context, req gen.ConnectSearchConsoleRequestObject) (gen.ConnectSearchConsoleResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	u, err := h.svc.ConnectURL(ctx, req.WorkspaceId, p.User.ID())
	if err != nil {
		return nil, err
	}
	return gen.ConnectSearchConsole200JSONResponse{Url: u}, nil
}

func (h *Handler) ChooseSearchConsoleProperty(ctx context.Context, req gen.ChooseSearchConsolePropertyRequestObject) (gen.ChooseSearchConsolePropertyResponseObject, error) {
	if _, err := h.svc.Choose(ctx, req.WorkspaceId, req.Body.Property); err != nil {
		return nil, err
	}
	s, err := h.status(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.ChooseSearchConsoleProperty200JSONResponse(s), nil
}

func (h *Handler) DisconnectSearchConsole(ctx context.Context, req gen.DisconnectSearchConsoleRequestObject) (gen.DisconnectSearchConsoleResponseObject, error) {
	if err := h.svc.Disconnect(ctx, req.WorkspaceId); err != nil {
		return nil, err
	}
	return gen.DisconnectSearchConsole204Response{}, nil
}

func (h *Handler) ListSearchConsoleProperties(ctx context.Context, req gen.ListSearchConsolePropertiesRequestObject) (gen.ListSearchConsolePropertiesResponseObject, error) {
	props, err := h.svc.Properties(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := gen.ListSearchConsoleProperties200JSONResponse{Properties: make([]gen.SearchConsoleProperty, 0, len(props))}
	for _, p := range props {
		out.Properties = append(out.Properties, gen.SearchConsoleProperty{Permission: p.Permission, Url: p.URL})
	}
	return out, nil
}

func (h *Handler) SyncSearchConsole(ctx context.Context, req gen.SyncSearchConsoleRequestObject) (gen.SyncSearchConsoleResponseObject, error) {
	if err := h.svc.SyncNow(ctx, req.WorkspaceId); err != nil {
		return nil, err
	}
	s, err := h.status(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.SyncSearchConsole202JSONResponse(s), nil
}

func (h *Handler) SearchConsoleCallback(ctx context.Context, req gen.SearchConsoleCallbackRequestObject) (gen.SearchConsoleCallbackResponseObject, error) {
	back := func(ws uuid.UUID, q url.Values) gen.SearchConsoleCallback302Response {
		loc := "/"
		if ws != uuid.Nil {
			loc = "/w/" + ws.String() + "/settings/publishing"
		}
		return gen.SearchConsoleCallback302Response{Headers: gen.SearchConsoleCallback302ResponseHeaders{Location: loc + "?" + q.Encode()}}
	}
	if req.Params.Error != nil {
		return back(h.svc.StateWorkspace(str(req.Params.State)), url.Values{"searchconsole": {"error"}, "reason": {*req.Params.Error}}), nil
	}
	ws, err := h.svc.Callback(ctx, str(req.Params.State), str(req.Params.Code))
	if err != nil {
		return back(ws, url.Values{"searchconsole": {"error"}, "reason": {err.Error()}}), nil
	}
	return back(ws, url.Values{"searchconsole": {"connected"}}), nil
}

func (h *Handler) GetSearchOverview(ctx context.Context, req gen.GetSearchOverviewRequestObject) (gen.GetSearchOverviewResponseObject, error) {
	days := 28
	if req.Params.Days != nil {
		days = *req.Params.Days
	}
	out := gen.GetSearchOverview200JSONResponse{Pages: []gen.SearchKey{}, Queries: []gen.SearchKey{}}
	c, err := h.svc.Status(ctx, req.WorkspaceId)
	if err != nil || c == nil || c.SyncedThrough().IsZero() {
		return out, err
	}
	o, err := h.svc.Overview(ctx, req.WorkspaceId, days, 20)
	if err != nil {
		return nil, err
	}
	start, end := openapi_types.Date{Time: o.Start}, openapi_types.Date{Time: o.End}
	cur, prev := totals(o.Current), totals(o.Previous)
	out.Start, out.End, out.Current, out.Previous = &start, &end, &cur, &prev
	out.Pages, out.Queries = keys(o.Pages), keys(o.Queries)
	return out, nil
}

func (h *Handler) GetStorySearch(ctx context.Context, req gen.GetStorySearchRequestObject) (gen.GetStorySearchResponseObject, error) {
	st, err := h.stories.Get(ctx, req.WorkspaceId, req.StoryId)
	if err != nil {
		return nil, err
	}
	perf, err := h.svc.Performance(ctx, req.WorkspaceId, livePages(st))
	if err != nil {
		return nil, err
	}
	out := gen.GetStorySearch200JSONResponse{Pages: make([]gen.PageSearch, 0, len(perf))}
	for _, p := range perf {
		out.Pages = append(out.Pages, gen.PageSearch{Url: p.URL, At: p.At, Through: openapi_types.Date{Time: p.Through},
			Before: totals(p.Before), After: totals(p.After)})
	}
	return out, nil
}

// livePages are the site pages a story put live: a published article's
// address, and the pages a published change set out to fix.
func livePages(st *storydomain.Story) []analyticssvc.Published {
	var out []analyticssvc.Published
	seen := map[string]bool{}
	add := func(u string, at time.Time) {
		if seen[u] || (!strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://")) {
			return
		}
		seen[u] = true
		out = append(out, analyticssvc.Published{URL: u, At: at})
	}
	for _, d := range st.Deliverables() {
		pub := d.Publication()
		if d.Status() != storydomain.StatusPublished || pub == nil {
			continue
		}
		switch {
		case d.IsChange():
			for _, f := range st.Brief().Findings {
				add(f.URL, pub.At)
			}
		case !d.IsVideo():
			add(pub.URL, pub.At)
		}
	}
	slices.SortFunc(out, func(a, b analyticssvc.Published) int { return strings.Compare(a.URL, b.URL) })
	return out
}

func totals(t analyticsdomain.Totals) gen.SearchTotals {
	return gen.SearchTotals{Clicks: t.Clicks, Impressions: t.Impressions, Position: float32(t.Position), Days: t.Days}
}

func keys(list []analyticssvc.KeyTotals) []gen.SearchKey {
	out := make([]gen.SearchKey, 0, len(list))
	for _, k := range list {
		out = append(out, gen.SearchKey{Key: k.Key, Totals: totals(k.Totals)})
	}
	return out
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

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
