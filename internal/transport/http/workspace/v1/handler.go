// Package handler implements the workspace operations of
// gen.StrictServerInterface.
package handler

import (
	"context"

	identitydomain "ekin/internal/domain/identity/v1"
	workspacedomain "ekin/internal/domain/workspace/v1"
	"ekin/internal/gen"
	workspacesvc "ekin/internal/service/workspace/v1"
	"ekin/internal/transport/http/principal"
)

// Handler serves workspaces.
type Handler struct {
	svc *workspacesvc.Service
}

// New returns a Handler.
func New(svc *workspacesvc.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListWorkspaces(ctx context.Context, _ gen.ListWorkspacesRequestObject) (gen.ListWorkspacesResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := h.svc.ListFor(ctx, p.User.ID())
	if err != nil {
		return nil, err
	}
	out := make([]gen.Workspace, 0, len(entries))
	for _, e := range entries {
		out = append(out, toWire(e.Workspace, e.Role))
	}
	return gen.ListWorkspaces200JSONResponse{Workspaces: out}, nil
}

func (h *Handler) CreateWorkspace(ctx context.Context, req gen.CreateWorkspaceRequestObject) (gen.CreateWorkspaceResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	loc, err := workspacedomain.ParseTimezone(req.Body.Timezone)
	if err != nil {
		return nil, err
	}
	w, err := h.svc.Create(ctx, p.User.ID(), req.Body.Name, loc)
	if err != nil {
		return nil, err
	}
	return gen.CreateWorkspace201JSONResponse(toWire(w, identitydomain.RoleOwner)), nil
}

func (h *Handler) GetWorkspace(ctx context.Context, req gen.GetWorkspaceRequestObject) (gen.GetWorkspaceResponseObject, error) {
	m, err := principal.Member(ctx)
	if err != nil {
		return nil, err
	}
	w, err := h.svc.Get(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetWorkspace200JSONResponse(toWire(w, m.Role())), nil
}

func (h *Handler) UpdateWorkspace(ctx context.Context, req gen.UpdateWorkspaceRequestObject) (gen.UpdateWorkspaceResponseObject, error) {
	m, err := principal.Member(ctx)
	if err != nil {
		return nil, err
	}
	change := workspacesvc.Change{Name: req.Body.Name}
	if req.Body.Timezone != nil {
		loc, err := workspacedomain.ParseTimezone(*req.Body.Timezone)
		if err != nil {
			return nil, err
		}
		change.Location = loc
	}
	w, err := h.svc.Update(ctx, req.WorkspaceId, change)
	if err != nil {
		return nil, err
	}
	return gen.UpdateWorkspace200JSONResponse(toWire(w, m.Role())), nil
}

func toWire(w *workspacedomain.Workspace, role identitydomain.Role) gen.Workspace {
	return gen.Workspace{
		Id:        w.ID(),
		Name:      w.Name(),
		Timezone:  w.Location().String(),
		Role:      gen.Role(role),
		CreatedAt: w.CreatedAt(),
		UpdatedAt: w.UpdatedAt(),
	}
}
