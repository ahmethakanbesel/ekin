// Package handler implements the SEO operations of
// gen.StrictServerInterface.
package handler

import (
	"cmp"
	"context"

	"github.com/google/uuid"

	seodomain "kurgu/internal/domain/seo/v1"
	"kurgu/internal/gen"
	seosvc "kurgu/internal/service/seo/v1"
	jobhandler "kurgu/internal/transport/http/job/v1"
	"kurgu/internal/transport/http/principal"
)

// Handler serves SEO opportunities.
type Handler struct{ svc *seosvc.Service }

// New returns a Handler.
func New(svc *seosvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) GetSEOProfile(ctx context.Context, req gen.GetSEOProfileRequestObject) (gen.GetSEOProfileResponseObject, error) {
	p, err := h.svc.Profile(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetSEOProfile200JSONResponse(profileToWire(p)), nil
}

func (h *Handler) SaveSEOProfile(ctx context.Context, req gen.SaveSEOProfileRequestObject) (gen.SaveSEOProfileResponseObject, error) {
	pr, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.SaveProfile(ctx, req.WorkspaceId, pr.User.ID(), seodomain.Profile{
		Domain: req.Body.Domain, Country: req.Body.Country, Language: string(req.Body.Language), Topics: req.Body.Topics,
	})
	if err != nil {
		return nil, err
	}
	return gen.SaveSEOProfile200JSONResponse(profileToWire(p)), nil
}

func (h *Handler) ListOpportunities(ctx context.Context, req gen.ListOpportunitiesRequestObject) (gen.ListOpportunitiesResponseObject, error) {
	var status seodomain.Status
	if req.Params.Status != nil {
		status = seodomain.Status(*req.Params.Status)
	}
	list, err := h.svc.List(ctx, req.WorkspaceId, status)
	if err != nil {
		return nil, err
	}
	out := gen.ListOpportunities200JSONResponse{Opportunities: []gen.Opportunity{}}
	for _, o := range list {
		out.Opportunities = append(out.Opportunities, toWire(o))
	}
	return out, nil
}

func (h *Handler) ImportOpportunities(ctx context.Context, req gen.ImportOpportunitiesRequestObject) (gen.ImportOpportunitiesResponseObject, error) {
	prof, err := h.svc.Profile(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	items := make([]seosvc.Item, 0, len(req.Body.Items))
	for _, it := range req.Body.Items {
		item := seosvc.Item{Keyword: it.Keyword, Language: prof.Language, Difficulty: -1}
		if it.Language != nil {
			item.Language = string(*it.Language)
		}
		if it.Volume != nil {
			item.Volume = *it.Volume
		}
		if it.Difficulty != nil {
			item.Difficulty = *it.Difficulty
		}
		item.Intent, item.Note = deref(it.Intent), deref(it.Note)
		items = append(items, item)
	}
	n, err := h.svc.Import(ctx, req.WorkspaceId, items, "manual")
	if err != nil {
		return nil, err
	}
	return gen.ImportOpportunities200JSONResponse{Added: n}, nil
}

func (h *Handler) RefreshOpportunities(ctx context.Context, req gen.RefreshOpportunitiesRequestObject) (gen.RefreshOpportunitiesResponseObject, error) {
	count := 20
	if req.Body != nil && req.Body.Count != nil {
		count = *req.Body.Count
	}
	j, err := h.svc.QueueRefresh(ctx, req.WorkspaceId, count)
	if err != nil {
		return nil, err
	}
	return gen.RefreshOpportunities202JSONResponse(jobhandler.ToWire(j)), nil
}

func (h *Handler) UpdateOpportunity(ctx context.Context, req gen.UpdateOpportunityRequestObject) (gen.UpdateOpportunityResponseObject, error) {
	o, err := h.svc.SetStatus(ctx, req.WorkspaceId, req.OpportunityId, seodomain.Status(req.Body.Status))
	if err != nil {
		return nil, err
	}
	return gen.UpdateOpportunity200JSONResponse(toWire(o)), nil
}

func profileToWire(p seodomain.Profile) gen.SEOProfile {
	return gen.SEOProfile{Domain: p.Domain, Country: p.Country, Language: gen.Language(cmp.Or(p.Language, "tr")), Topics: p.Topics}
}

func toWire(o seodomain.Opportunity) gen.Opportunity {
	w := gen.Opportunity{
		Id: o.ID, Keyword: o.Keyword, Language: gen.Language(o.Language), Volume: o.Volume, Difficulty: o.Difficulty,
		Intent: o.Intent, Note: o.Note, Source: o.Source, Status: gen.OpportunityStatus(o.Status),
		Score: float32(o.Score()), CreatedAt: o.CreatedAt,
	}
	if o.StoryID != uuid.Nil {
		sid := o.StoryID
		w.StoryId = &sid
	}
	return w
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
