package handler

import (
	"context"

	sitedomain "github.com/ahmethakanbesel/ekin/internal/domain/site/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	sitesvc "github.com/ahmethakanbesel/ekin/internal/service/site/v1"
)

func (h *Handler) ListCompetitors(ctx context.Context, req gen.ListCompetitorsRequestObject) (gen.ListCompetitorsResponseObject, error) {
	list, err := h.svc.Competitors(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := gen.ListCompetitors200JSONResponse{Competitors: make([]gen.Competitor, 0, len(list))}
	for _, c := range list {
		out.Competitors = append(out.Competitors, competitorToWire(c))
	}
	return out, nil
}

func (h *Handler) AddCompetitor(ctx context.Context, req gen.AddCompetitorRequestObject) (gen.AddCompetitorResponseObject, error) {
	name := ""
	if req.Body.Name != nil {
		name = *req.Body.Name
	}
	c, err := h.svc.AddCompetitor(ctx, req.WorkspaceId, name, req.Body.Url)
	if err != nil {
		return nil, err
	}
	return gen.AddCompetitor201JSONResponse(competitorToWire(c)), nil
}

func (h *Handler) RemoveCompetitor(ctx context.Context, req gen.RemoveCompetitorRequestObject) (gen.RemoveCompetitorResponseObject, error) {
	if err := h.svc.RemoveCompetitor(ctx, req.WorkspaceId, req.CompetitorId); err != nil {
		return nil, err
	}
	return gen.RemoveCompetitor204Response{}, nil
}

func (h *Handler) CheckCompetitor(ctx context.Context, req gen.CheckCompetitorRequestObject) (gen.CheckCompetitorResponseObject, error) {
	c, err := h.svc.CheckCompetitor(ctx, req.WorkspaceId, req.CompetitorId)
	if err != nil {
		return nil, err
	}
	return gen.CheckCompetitor202JSONResponse(competitorToWire(c)), nil
}

func (h *Handler) ListCompetitorChanges(ctx context.Context, req gen.ListCompetitorChangesRequestObject) (gen.ListCompetitorChangesResponseObject, error) {
	var f sitesvc.ChangeFilter
	if req.Params.CompetitorId != nil {
		f.CompetitorID = *req.Params.CompetitorId
	}
	if req.Params.Kind != nil {
		f.Kind = sitedomain.ChangeKind(*req.Params.Kind)
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		f.After = *req.Params.Cursor
	}
	list, next, err := h.svc.CompetitorChanges(ctx, req.WorkspaceId, f)
	if err != nil {
		return nil, err
	}
	out := gen.ListCompetitorChanges200JSONResponse{Changes: make([]gen.CompetitorChange, 0, len(list))}
	for _, c := range list {
		out.Changes = append(out.Changes, gen.CompetitorChange{Id: c.ID, CompetitorId: c.CompetitorID, Kind: gen.CompetitorChangeKind(c.Kind),
			Url: c.URL, Title: c.Title, At: c.At})
	}
	if next != "" {
		out.NextCursor = &next
	}
	return out, nil
}

func competitorToWire(c *sitedomain.Competitor) gen.Competitor {
	return gen.Competitor{Id: c.ID(), Name: c.Name(), Url: c.URL(), Pages: c.Pages(), LastCheckAt: timePtr(c.LastCheckAt()),
		LastError: strPtr(c.LastError()), NextCheckAt: c.NextCheckAt(), CreatedAt: c.CreatedAt()}
}
