// Package handler implements the template operations of
// gen.StrictServerInterface.
package handler

import (
	"context"

	pipelinedomain "kurgu/internal/domain/pipeline/v1"
	templatedomain "kurgu/internal/domain/template/v1"
	"kurgu/internal/gen"
	templatesvc "kurgu/internal/service/template/v1"
	jobhandler "kurgu/internal/transport/http/job/v1"
)

// Handler serves templates.
type Handler struct{ svc *templatesvc.Service }

// New returns a Handler.
func New(svc *templatesvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListTemplates(ctx context.Context, req gen.ListTemplatesRequestObject) (gen.ListTemplatesResponseObject, error) {
	list, err := h.svc.List(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := gen.ListTemplates200JSONResponse{Templates: make([]gen.Template, 0, len(list))}
	for _, t := range list {
		out.Templates = append(out.Templates, ToWire(t))
	}
	return out, nil
}

func (h *Handler) GetTemplate(ctx context.Context, req gen.GetTemplateRequestObject) (gen.GetTemplateResponseObject, error) {
	t, err := h.svc.Get(ctx, req.WorkspaceId, req.TemplateId)
	if err != nil {
		return nil, err
	}
	return gen.GetTemplate200JSONResponse(ToWire(t)), nil
}

func (h *Handler) CreateTemplate(ctx context.Context, req gen.CreateTemplateRequestObject) (gen.CreateTemplateResponseObject, error) {
	t, err := h.svc.Create(ctx, req.WorkspaceId, specOf(*req.Body))
	if err != nil {
		return nil, err
	}
	return gen.CreateTemplate201JSONResponse(ToWire(t)), nil
}

func (h *Handler) UpdateTemplate(ctx context.Context, req gen.UpdateTemplateRequestObject) (gen.UpdateTemplateResponseObject, error) {
	t, err := h.svc.Update(ctx, req.WorkspaceId, req.TemplateId, specOf(*req.Body))
	if err != nil {
		return nil, err
	}
	return gen.UpdateTemplate200JSONResponse(ToWire(t)), nil
}

func (h *Handler) DeleteTemplate(ctx context.Context, req gen.DeleteTemplateRequestObject) (gen.DeleteTemplateResponseObject, error) {
	if err := h.svc.Delete(ctx, req.WorkspaceId, req.TemplateId); err != nil {
		return nil, err
	}
	return gen.DeleteTemplate204Response{}, nil
}

func (h *Handler) DuplicateTemplate(ctx context.Context, req gen.DuplicateTemplateRequestObject) (gen.DuplicateTemplateResponseObject, error) {
	t, err := h.svc.Duplicate(ctx, req.WorkspaceId, req.TemplateId)
	if err != nil {
		return nil, err
	}
	return gen.DuplicateTemplate201JSONResponse(ToWire(t)), nil
}

func (h *Handler) GenerateTemplate(ctx context.Context, req gen.GenerateTemplateRequestObject) (gen.GenerateTemplateResponseObject, error) {
	var kinds []pipelinedomain.Kind
	if req.Body.Kinds != nil {
		for _, k := range *req.Body.Kinds {
			kinds = append(kinds, pipelinedomain.Kind(k))
		}
	}
	notes := ""
	if req.Body.Notes != nil {
		notes = *req.Body.Notes
	}
	j, err := h.svc.QueueGenerate(ctx, req.WorkspaceId, req.Body.Channel, notes, kinds)
	if err != nil {
		return nil, err
	}
	return gen.GenerateTemplate202JSONResponse(jobhandler.ToWire(j)), nil
}

func specOf(in gen.TemplateInput) templatedomain.Spec {
	s := templatedomain.Spec{Name: in.Name, Guide: in.Guide}
	for _, k := range in.Kinds {
		s.Kinds = append(s.Kinds, pipelinedomain.Kind(k))
	}
	if in.Description != nil {
		s.Description = *in.Description
	}
	if in.Inspiration != nil {
		s.Inspiration = *in.Inspiration
	}
	if in.Palette != nil {
		s.Palette = *in.Palette
	}
	if in.HeadingFont != nil {
		s.HeadingFont = string(*in.HeadingFont)
	}
	if in.BodyFont != nil {
		s.BodyFont = string(*in.BodyFont)
	}
	if in.MusicMood != nil {
		s.MusicMood = *in.MusicMood
	}
	return s
}

// ToWire converts a template.
func ToWire(t templatedomain.Template) gen.Template {
	kinds := make([]gen.DeliverableKind, 0, len(t.Kinds))
	for _, k := range t.Kinds {
		kinds = append(kinds, gen.DeliverableKind(k))
	}
	palette := t.Palette
	if palette == nil {
		palette = []string{}
	}
	return gen.Template{
		Id: t.ID, Name: t.Name, Description: t.Description, Inspiration: t.Inspiration, Kinds: kinds, Guide: t.Guide,
		Palette: palette, HeadingFont: gen.TemplateFont(t.HeadingFont), BodyFont: gen.TemplateFont(t.BodyFont),
		MusicMood: t.MusicMood, Source: gen.TemplateSource(t.Source), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}
