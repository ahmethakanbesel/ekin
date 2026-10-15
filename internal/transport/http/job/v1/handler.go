// Package handler implements the job operations of
// gen.StrictServerInterface.
package handler

import (
	"context"
	"encoding/json"

	jobdomain "kurgu/internal/domain/job/v1"
	"kurgu/internal/gen"
	jobsvc "kurgu/internal/service/job/v1"
)

// Handler serves jobs.
type Handler struct {
	svc *jobsvc.Service
}

// New returns a Handler.
func New(svc *jobsvc.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListJobs(ctx context.Context, req gen.ListJobsRequestObject) (gen.ListJobsResponseObject, error) {
	var f jobsvc.Filter
	if req.Params.Status != nil {
		for _, raw := range *req.Params.Status {
			s, err := jobdomain.ParseStatus(string(raw))
			if err != nil {
				return nil, err
			}
			f.Statuses = append(f.Statuses, s)
		}
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	jobs, err := h.svc.List(ctx, req.WorkspaceId, f)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Job, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, ToWire(j))
	}
	return gen.ListJobs200JSONResponse{Jobs: out}, nil
}

func (h *Handler) GetJob(ctx context.Context, req gen.GetJobRequestObject) (gen.GetJobResponseObject, error) {
	j, err := h.svc.Get(ctx, req.WorkspaceId, req.JobId)
	if err != nil {
		return nil, err
	}
	return gen.GetJob200JSONResponse(ToWire(j)), nil
}

func (h *Handler) CancelJob(ctx context.Context, req gen.CancelJobRequestObject) (gen.CancelJobResponseObject, error) {
	j, err := h.svc.Cancel(ctx, req.WorkspaceId, req.JobId)
	if err != nil {
		return nil, err
	}
	return gen.CancelJob200JSONResponse(ToWire(j)), nil
}

// ToWire maps a job to its API shape. Other handlers that return a job
// (a manual pipeline run) reuse it.
func ToWire(j *jobdomain.Job) gen.Job {
	out := gen.Job{
		Id:          j.ID(),
		WorkspaceId: j.WorkspaceID(),
		TraceId:     j.TraceID(),
		Queue:       string(j.Queue()),
		Kind:        j.Kind(),
		Status:      gen.JobStatus(j.Status()),
		Attempt:     j.Attempt(),
		MaxAttempts: j.MaxAttempts(),
		RunAfter:    j.RunAfter(),
		CreatedAt:   j.CreatedAt(),
		UpdatedAt:   j.UpdatedAt(),
	}
	if e := j.LastError(); e != "" {
		out.LastError = &e
	}
	if t := j.StartedAt(); !t.IsZero() {
		out.StartedAt = &t
	}
	if f := j.FinishedAt(); !f.IsZero() {
		out.FinishedAt = &f
	}
	// Payloads name their subject with these keys by convention; a job
	// without them has an empty subject.
	_ = json.Unmarshal(j.Payload(), &out.Subject)
	return out
}
