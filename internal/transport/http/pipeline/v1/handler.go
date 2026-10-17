// Package handler implements the pipeline operations of
// gen.StrictServerInterface.
package handler

import (
	"context"

	"ekin/internal/gen"
	pipelinesvc "ekin/internal/service/pipeline/v1"
	jobhandler "ekin/internal/transport/http/job/v1"
)

// Handler serves pipelines.
type Handler struct {
	svc *pipelinesvc.Service
}

// New returns a Handler.
func New(svc *pipelinesvc.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListPipelines(ctx context.Context, req gen.ListPipelinesRequestObject) (gen.ListPipelinesResponseObject, error) {
	ps, err := h.svc.List(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Pipeline, 0, len(ps))
	for _, p := range ps {
		out = append(out, toWire(p))
	}
	return gen.ListPipelines200JSONResponse{Pipelines: out}, nil
}

func (h *Handler) CreatePipeline(ctx context.Context, req gen.CreatePipelineRequestObject) (gen.CreatePipelineResponseObject, error) {
	def, err := parseDefinition(*req.Body)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.Create(ctx, req.WorkspaceId, def)
	if err != nil {
		return nil, err
	}
	return gen.CreatePipeline201JSONResponse(toWire(p)), nil
}

func (h *Handler) GetPipeline(ctx context.Context, req gen.GetPipelineRequestObject) (gen.GetPipelineResponseObject, error) {
	p, err := h.svc.Get(ctx, req.WorkspaceId, req.PipelineId)
	if err != nil {
		return nil, err
	}
	return gen.GetPipeline200JSONResponse(toWire(p)), nil
}

func (h *Handler) RedefinePipeline(ctx context.Context, req gen.RedefinePipelineRequestObject) (gen.RedefinePipelineResponseObject, error) {
	def, err := parseDefinition(req.Body.Definition)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.Redefine(ctx, req.WorkspaceId, req.PipelineId, req.Body.Version, def)
	if err != nil {
		return nil, err
	}
	return gen.RedefinePipeline200JSONResponse(toWire(p)), nil
}

func (h *Handler) DeletePipeline(ctx context.Context, req gen.DeletePipelineRequestObject) (gen.DeletePipelineResponseObject, error) {
	if err := h.svc.Delete(ctx, req.WorkspaceId, req.PipelineId); err != nil {
		return nil, err
	}
	return gen.DeletePipeline204Response{}, nil
}

func (h *Handler) PausePipeline(ctx context.Context, req gen.PausePipelineRequestObject) (gen.PausePipelineResponseObject, error) {
	p, err := h.svc.Pause(ctx, req.WorkspaceId, req.PipelineId)
	if err != nil {
		return nil, err
	}
	return gen.PausePipeline200JSONResponse(toWire(p)), nil
}

func (h *Handler) ResumePipeline(ctx context.Context, req gen.ResumePipelineRequestObject) (gen.ResumePipelineResponseObject, error) {
	p, err := h.svc.Resume(ctx, req.WorkspaceId, req.PipelineId)
	if err != nil {
		return nil, err
	}
	return gen.ResumePipeline200JSONResponse(toWire(p)), nil
}

func (h *Handler) RunPipeline(ctx context.Context, req gen.RunPipelineRequestObject) (gen.RunPipelineResponseObject, error) {
	j, err := h.svc.RunNow(ctx, req.WorkspaceId, req.PipelineId, req.Body.Count)
	if err != nil {
		return nil, err
	}
	return gen.RunPipeline202JSONResponse(jobhandler.ToWire(j)), nil
}

func (h *Handler) ListUpcomingRuns(ctx context.Context, req gen.ListUpcomingRunsRequestObject) (gen.ListUpcomingRunsResponseObject, error) {
	days := 7
	if req.Params.Days != nil {
		days = *req.Params.Days
	}
	runs, until, err := h.svc.Upcoming(ctx, req.WorkspaceId, days)
	if err != nil {
		return nil, err
	}
	out := make([]gen.UpcomingRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, gen.UpcomingRun{PipelineId: r.PipelineID, At: r.At, Count: r.Count})
	}
	return gen.ListUpcomingRuns200JSONResponse{Until: until, Runs: out}, nil
}
