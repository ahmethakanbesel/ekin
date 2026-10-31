// Package handler implements the AI visibility operations of
// gen.StrictServerInterface.
package handler

import (
	"context"
	"time"

	"github.com/google/uuid"

	visibilitydomain "github.com/ahmethakanbesel/ekin/internal/domain/visibility/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	visibilitysvc "github.com/ahmethakanbesel/ekin/internal/service/visibility/v1"
	jobhandler "github.com/ahmethakanbesel/ekin/internal/transport/http/job/v1"
)

// Handler serves AI visibility.
type Handler struct{ svc *visibilitysvc.Service }

// New returns a Handler.
func New(svc *visibilitysvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) overview(ctx context.Context, ws uuid.UUID) (gen.VisibilityOverview, error) {
	o, err := h.svc.Overview(ctx, ws)
	if err != nil {
		return gen.VisibilityOverview{}, err
	}
	out := gen.VisibilityOverview{Ready: o.Ready, Suggested: settingsToWire(o.Suggested), Prompts: make([]gen.VisibilityPrompt, 0, len(o.Prompts)),
		Runs: make([]gen.VisibilityRun, 0, len(o.Runs))}
	if o.Profile != nil {
		st := settingsToWire(o.Profile.Settings())
		out.Settings, out.NextRunAt = &st, timePtr(o.Profile.NextRunAt())
	}
	for _, p := range o.Prompts {
		out.Prompts = append(out.Prompts, promptToWire(p))
	}
	for _, r := range o.Runs {
		out.Runs = append(out.Runs, runToWire(r))
	}
	e := o.Estimate
	out.Estimate.Prompts, out.Estimate.Models, out.Estimate.Calls, out.Estimate.Used, out.Estimate.Cap = e.Prompts, e.Models, e.Calls, e.Used, e.Cap
	return out, nil
}

func (h *Handler) GetVisibility(ctx context.Context, req gen.GetVisibilityRequestObject) (gen.GetVisibilityResponseObject, error) {
	o, err := h.overview(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetVisibility200JSONResponse(o), nil
}

func (h *Handler) SaveVisibility(ctx context.Context, req gen.SaveVisibilityRequestObject) (gen.SaveVisibilityResponseObject, error) {
	if _, err := h.svc.SaveSettings(ctx, req.WorkspaceId, settingsFromWire(*req.Body)); err != nil {
		return nil, err
	}
	o, err := h.overview(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.SaveVisibility200JSONResponse(o), nil
}

func (h *Handler) AddVisibilityPrompt(ctx context.Context, req gen.AddVisibilityPromptRequestObject) (gen.AddVisibilityPromptResponseObject, error) {
	p, err := h.svc.AddPrompt(ctx, req.WorkspaceId, req.Body.Text)
	if err != nil {
		return nil, err
	}
	return gen.AddVisibilityPrompt201JSONResponse(promptToWire(p)), nil
}

func (h *Handler) DraftVisibilityPrompts(ctx context.Context, req gen.DraftVisibilityPromptsRequestObject) (gen.DraftVisibilityPromptsResponseObject, error) {
	j, err := h.svc.QueueDraft(ctx, req.WorkspaceId, req.Body.Count)
	if err != nil {
		return nil, err
	}
	return gen.DraftVisibilityPrompts202JSONResponse(jobhandler.ToWire(j)), nil
}

func (h *Handler) UpdateVisibilityPrompt(ctx context.Context, req gen.UpdateVisibilityPromptRequestObject) (gen.UpdateVisibilityPromptResponseObject, error) {
	p, err := h.svc.UpdatePrompt(ctx, req.WorkspaceId, req.PromptId, req.Body.Text, req.Body.Active)
	if err != nil {
		return nil, err
	}
	return gen.UpdateVisibilityPrompt200JSONResponse(promptToWire(p)), nil
}

func (h *Handler) DeleteVisibilityPrompt(ctx context.Context, req gen.DeleteVisibilityPromptRequestObject) (gen.DeleteVisibilityPromptResponseObject, error) {
	if err := h.svc.DeletePrompt(ctx, req.WorkspaceId, req.PromptId); err != nil {
		return nil, err
	}
	return gen.DeleteVisibilityPrompt204Response{}, nil
}

func (h *Handler) StartVisibilityRun(ctx context.Context, req gen.StartVisibilityRunRequestObject) (gen.StartVisibilityRunResponseObject, error) {
	r, err := h.svc.StartRun(ctx, req.WorkspaceId, "manual")
	if err != nil {
		return nil, err
	}
	return gen.StartVisibilityRun201JSONResponse(runToWire(r)), nil
}

func (h *Handler) GetVisibilityReport(ctx context.Context, req gen.GetVisibilityReportRequestObject) (gen.GetVisibilityReportResponseObject, error) {
	var runID uuid.UUID
	if req.Params.RunId != nil {
		runID = *req.Params.RunId
	}
	rep, err := h.svc.Report(ctx, req.WorkspaceId, runID)
	if err != nil {
		return nil, err
	}
	out := gen.GetVisibilityReport200JSONResponse{Models: []gen.VisibilityModel{}, Voices: []gen.VisibilityVoice{}, Domains: []gen.VisibilityDomain{},
		Answers: []gen.VisibilityAnswer{}}
	if rep.Run.ID == uuid.Nil {
		return out, nil
	}
	run := runToWire(rep.Run)
	out.Run = &run
	for _, m := range rep.Models {
		out.Models = append(out.Models, gen.VisibilityModel{Model: m.Model, Answers: m.Answers, Mentioned: m.Mentioned, Cited: m.Cited,
			AvgPosition: float32(m.AvgPosition)})
	}
	for _, v := range rep.Voices {
		out.Voices = append(out.Voices, gen.VisibilityVoice{Name: v.Name, Mentions: v.Mentions, Cited: v.Cited, Own: v.Own})
	}
	for _, d := range rep.Domains {
		out.Domains = append(out.Domains, gen.VisibilityDomain{Domain: d.Domain, Count: d.Count})
	}
	for _, a := range rep.Answers {
		w := gen.VisibilityAnswer{Id: a.ID, PromptId: a.PromptID, Model: a.Model, Text: a.Text, Citations: nonNil(a.Citations),
			Mentioned: a.Mentioned, Position: a.Position, Cited: a.Cited, Competitors: nonNil(a.Competitors), CompetitorsCited: nonNil(a.CompetitorsCited)}
		if a.Error != "" {
			e := a.Error
			w.Error = &e
		}
		out.Answers = append(out.Answers, w)
	}
	return out, nil
}

func brandToWire(b visibilitydomain.Brand) gen.VisibilityBrand {
	out := gen.VisibilityBrand{Name: b.Name}
	if len(b.Aliases) > 0 {
		a := append([]string(nil), b.Aliases...)
		out.Aliases = &a
	}
	if b.Domain != "" {
		d := b.Domain
		out.Domain = &d
	}
	return out
}

func brandFromWire(b gen.VisibilityBrand) visibilitydomain.Brand {
	out := visibilitydomain.Brand{Name: b.Name}
	if b.Aliases != nil {
		out.Aliases = *b.Aliases
	}
	if b.Domain != nil {
		out.Domain = *b.Domain
	}
	return out
}

func settingsToWire(st visibilitydomain.Settings) gen.VisibilitySettings {
	out := gen.VisibilitySettings{Brand: brandToWire(st.Brand), Competitors: []gen.VisibilityBrand{}, Models: nonNil(st.Models)}
	for _, c := range st.Competitors {
		out.Competitors = append(out.Competitors, brandToWire(c))
	}
	if st.Schedule != "" {
		sc := gen.VisibilitySettingsSchedule(st.Schedule)
		out.Schedule = &sc
	}
	if st.MonthlyCap > 0 {
		c := st.MonthlyCap
		out.MonthlyCap = &c
	}
	return out
}

func settingsFromWire(in gen.VisibilitySettings) visibilitydomain.Settings {
	st := visibilitydomain.Settings{Brand: brandFromWire(in.Brand), Models: in.Models}
	for _, c := range in.Competitors {
		st.Competitors = append(st.Competitors, brandFromWire(c))
	}
	if in.Schedule != nil {
		st.Schedule = visibilitydomain.Schedule(*in.Schedule)
	}
	if in.MonthlyCap != nil {
		st.MonthlyCap = *in.MonthlyCap
	}
	return st
}

func promptToWire(p visibilitydomain.Prompt) gen.VisibilityPrompt {
	out := gen.VisibilityPrompt{Id: p.ID, Text: p.Text, Source: gen.VisibilityPromptSource(p.Source), Active: p.Active, CreatedAt: p.CreatedAt,
		CoveredAt: timePtr(p.CoveredAt)}
	if p.CoveredBy != uuid.Nil {
		c := p.CoveredBy
		out.CoveredBy = &c
	}
	return out
}

func runToWire(r visibilitydomain.Run) gen.VisibilityRun {
	out := gen.VisibilityRun{Id: r.ID, Status: gen.VisibilityRunStatus(r.Status), Trigger: r.Trigger, Planned: r.Planned, Done: r.Done,
		Failed: r.Failed, Cost: float32(r.Cost), CreatedAt: r.CreatedAt, StartedAt: timePtr(r.StartedAt), FinishedAt: timePtr(r.FinishedAt)}
	if r.Failure != "" {
		f := r.Failure
		out.Failure = &f
	}
	return out
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
