// Package handler implements the workspace operations of
// gen.StrictServerInterface.
package handler

import (
	"context"

	workspacedomain "kurgu/internal/domain/workspace/v1"
	"kurgu/internal/gen"
	workspacesvc "kurgu/internal/service/workspace/v1"
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
	ws, err := h.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Workspace, 0, len(ws))
	for _, w := range ws {
		out = append(out, toWire(w))
	}
	return gen.ListWorkspaces200JSONResponse{Workspaces: out}, nil
}

func (h *Handler) CreateWorkspace(ctx context.Context, req gen.CreateWorkspaceRequestObject) (gen.CreateWorkspaceResponseObject, error) {
	loc, err := workspacedomain.ParseTimezone(req.Body.Timezone)
	if err != nil {
		return nil, err
	}
	w, err := h.svc.Create(ctx, req.Body.Name, loc)
	if err != nil {
		return nil, err
	}
	return gen.CreateWorkspace201JSONResponse(toWire(w)), nil
}

func (h *Handler) GetWorkspace(ctx context.Context, req gen.GetWorkspaceRequestObject) (gen.GetWorkspaceResponseObject, error) {
	w, err := h.svc.Get(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetWorkspace200JSONResponse(toWire(w)), nil
}

func (h *Handler) UpdateWorkspace(ctx context.Context, req gen.UpdateWorkspaceRequestObject) (gen.UpdateWorkspaceResponseObject, error) {
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
	return gen.UpdateWorkspace200JSONResponse(toWire(w)), nil
}

func toWire(w *workspacedomain.Workspace) gen.Workspace {
	return gen.Workspace{
		Id:        w.ID(),
		Name:      w.Name(),
		Timezone:  w.Location().String(),
		CreatedAt: w.CreatedAt(),
		UpdatedAt: w.UpdatedAt(),
	}
}
