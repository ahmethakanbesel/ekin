// Package handler implements the SEO operations of
// gen.StrictServerInterface.
package handler

import (
	"cmp"
	"context"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	seodomain "github.com/ahmethakanbesel/ekin/internal/domain/seo/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	seosvc "github.com/ahmethakanbesel/ekin/internal/service/seo/v1"
	jobhandler "github.com/ahmethakanbesel/ekin/internal/transport/http/job/v1"
	"github.com/ahmethakanbesel/ekin/internal/transport/http/principal"
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
	b := req.Body
	p, err := h.svc.SaveProfile(ctx, req.WorkspaceId, pr.User.ID(), seodomain.Profile{
		Domain: b.Domain, Country: b.Country, Language: string(b.Language), Topics: b.Topics,
		ProjectID: deref(b.ProjectId), Seeds: derefList(b.Seeds), Competitors: derefList(b.Competitors),
		MaxDifficulty: derefInt(b.MaxDifficulty), MinVolume: derefInt(b.MinVolume),
	})
	if err != nil {
		return nil, err
	}
	return gen.SaveSEOProfile200JSONResponse(profileToWire(p)), nil
}

func (h *Handler) ListOpportunities(ctx context.Context, req gen.ListOpportunitiesRequestObject) (gen.ListOpportunitiesResponseObject, error) {
	var f seosvc.Filter
	if req.Params.Status != nil {
		for _, raw := range *req.Params.Status {
			st, err := seodomain.ParseStatus(string(raw))
			if err != nil {
				return nil, err
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	if req.Params.Language != nil {
		if !req.Params.Language.Valid() {
			return nil, apperror.New(apperror.ValidationFailed, "language must be tr or en")
		}
		f.Language = string(*req.Params.Language)
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
	list, next, err := h.svc.List(ctx, req.WorkspaceId, f)
	if err != nil {
		return nil, err
	}
	out := gen.ListOpportunities200JSONResponse{Opportunities: []gen.Opportunity{}}
	for _, o := range list {
		out.Opportunities = append(out.Opportunities, toWire(o))
	}
	if next != "" {
		out.NextCursor = &next
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
	seeds, competitors, est := p.Seeds, p.Competitors, p.EstimatedCredits()
	if seeds == nil {
		seeds = []string{}
	}
	if competitors == nil {
		competitors = []string{}
	}
	maxDifficulty, minVolume := p.Difficulty(), p.MinVolume
	return gen.SEOProfile{
		Domain: p.Domain, Country: p.Country, Language: gen.Language(cmp.Or(p.Language, "en")), Topics: p.Topics,
		ProjectId: &p.ProjectID, Seeds: &seeds, Competitors: &competitors, MaxDifficulty: &maxDifficulty, MinVolume: &minVolume,
		EstimatedCredits: &est,
	}
}

func (h *Handler) GetOpenSEOStatus(ctx context.Context, _ gen.GetOpenSEOStatusRequestObject) (gen.GetOpenSEOStatusResponseObject, error) {
	st := h.svc.OpenSEO(ctx)
	out := gen.GetOpenSEOStatus200JSONResponse{Configured: st.Configured, Projects: []gen.OpenSEOProject{}}
	if st.Error != "" {
		out.Error = &st.Error
	}
	if a := st.Account; a != nil {
		out.Email, out.CreditsRemaining = &a.UserEmail, a.CreditsRemaining
	}
	for _, p := range st.Projects {
		out.Projects = append(out.Projects, gen.OpenSEOProject{
			Id: p.ID, Name: p.Name, Domain: &p.Domain, LocationCode: p.LocationCode, LanguageCode: p.LanguageCode, Url: &p.URL,
			Organization: &p.Organization,
		})
	}
	return out, nil
}

func derefList(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
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
