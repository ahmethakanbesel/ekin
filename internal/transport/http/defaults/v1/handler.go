// Package handler implements the workspace production-defaults
// operations of gen.StrictServerInterface.
package handler

import (
	"context"

	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	"kurgu/internal/gen"
	defaultssvc "kurgu/internal/service/defaults/v1"
	pipelinehandler "kurgu/internal/transport/http/pipeline/v1"
	"kurgu/internal/transport/http/principal"
)

// Handler serves a workspace's production defaults.
type Handler struct{ svc *defaultssvc.Service }

// New returns a Handler.
func New(svc *defaultssvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) GetWorkspaceProduction(ctx context.Context, req gen.GetWorkspaceProductionRequestObject) (gen.GetWorkspaceProductionResponseObject, error) {
	p, err := h.svc.Get(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out, err := h.toWire(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.GetWorkspaceProduction200JSONResponse(out), nil
}

func (h *Handler) SaveWorkspaceProduction(ctx context.Context, req gen.SaveWorkspaceProductionRequestObject) (gen.SaveWorkspaceProductionResponseObject, error) {
	in, err := pipelinehandler.ProductionOf(&req.Body.Defaults, req.Body.SelfReviewRounds)
	if err != nil {
		return nil, err
	}
	pr, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.Put(ctx, req.WorkspaceId, in, pr.User.ID())
	if err != nil {
		return nil, err
	}
	out, err := h.toWire(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.SaveWorkspaceProduction200JSONResponse(out), nil
}

func (h *Handler) toWire(ctx context.Context, p pipelinedomain.Production) (gen.WorkspaceProduction, error) {
	srv, err := h.svc.Server(ctx)
	if err != nil {
		return gen.WorkspaceProduction{}, err
	}
	out := gen.WorkspaceProduction{SelfReviewRounds: p.SelfReview.Wire(), Server: serverToWire(srv)}
	if d := pipelinehandler.ProductionToWire(p); d != nil {
		out.Defaults = *d
	}
	return out, nil
}

func serverToWire(s defaultssvc.Server) gen.ServerProduction {
	out := gen.ServerProduction{
		DraftModelSource: gen.ServerProductionDraftModelSource(s.DraftModelSource),
		PreviewHeight:    s.PreviewHeight, FinalHeight: s.FinalHeight,
		PreviewFps: s.PreviewFPS, FinalFps: s.FinalFPS,
		Profile: s.Profile,
	}
	if s.DraftModel != "" {
		out.DraftModel = &s.DraftModel
	}
	if s.DraftEffort != "" {
		e := gen.DraftEffort(s.DraftEffort)
		out.DraftEffort = &e
	}
	return out
}
