// Package handler implements the integrations operations of
// gen.StrictServerInterface: the YouTube channel and the blog target.
package handler

import (
	"context"
	"net/url"

	"github.com/google/uuid"

	publishingdomain "kurgu/internal/domain/publishing/v1"
	"kurgu/internal/gen"
	publishingsvc "kurgu/internal/service/publishing/v1"
	"kurgu/internal/transport/http/principal"
)

// Handler serves integrations.
type Handler struct {
	svc *publishingsvc.Service
}

// New returns a Handler.
func New(svc *publishingsvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) integrations(ctx context.Context, ws uuid.UUID) (gen.Integrations, error) {
	out := gen.Integrations{Youtube: gen.YouTubeIntegration{Configured: h.svc.YouTubeConfigured()}}
	ch, ok, err := h.svc.Channel(ctx, ws)
	if err != nil {
		return out, err
	}
	if ok {
		privacy := gen.Privacy(ch.Privacy)
		at := ch.ConnectedAt
		out.Youtube.Connected, out.Youtube.ChannelId, out.Youtube.ChannelTitle = true, &ch.ChannelID, &ch.Title
		out.Youtube.Privacy, out.Youtube.ConnectedAt = &privacy, &at
	}
	b, ok, err := h.svc.Blog(ctx, ws)
	if err != nil {
		return out, err
	}
	if ok {
		out.Blog = &gen.BlogTarget{
			Kind: gen.BlogTargetKind(b.Kind), Repo: &b.Repo, Branch: &b.Branch, Dir: &b.Dir,
			ContentDir: b.ContentDir, PublicDir: b.PublicDir, SiteUrl: b.SiteURL, PathTemplate: b.PathTemplate,
			DefaultLocale: b.DefaultLocale, Author: b.Author, Draft: b.Draft, HasToken: b.SealedToken != "",
		}
	}
	return out, nil
}

func (h *Handler) GetIntegrations(ctx context.Context, req gen.GetIntegrationsRequestObject) (gen.GetIntegrationsResponseObject, error) {
	out, err := h.integrations(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetIntegrations200JSONResponse(out), nil
}

func (h *Handler) ConnectYouTube(ctx context.Context, req gen.ConnectYouTubeRequestObject) (gen.ConnectYouTubeResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	link, err := h.svc.ConnectURL(ctx, req.WorkspaceId, p.User.ID())
	if err != nil {
		return nil, err
	}
	return gen.ConnectYouTube200JSONResponse{Url: link}, nil
}

func (h *Handler) UpdateYouTube(ctx context.Context, req gen.UpdateYouTubeRequestObject) (gen.UpdateYouTubeResponseObject, error) {
	privacy, err := publishingdomain.ParsePrivacy(string(req.Body.Privacy))
	if err != nil {
		return nil, err
	}
	if _, err := h.svc.SetPrivacy(ctx, req.WorkspaceId, privacy); err != nil {
		return nil, err
	}
	out, err := h.integrations(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.UpdateYouTube200JSONResponse(out), nil
}

func (h *Handler) DisconnectYouTube(ctx context.Context, req gen.DisconnectYouTubeRequestObject) (gen.DisconnectYouTubeResponseObject, error) {
	if err := h.svc.Disconnect(ctx, req.WorkspaceId); err != nil {
		return nil, err
	}
	return gen.DisconnectYouTube204Response{}, nil
}

func (h *Handler) SaveBlog(ctx context.Context, req gen.SaveBlogRequestObject) (gen.SaveBlogResponseObject, error) {
	b := req.Body
	in := publishingsvc.BlogInput{BlogTarget: publishingdomain.BlogTarget{
		Kind: publishingdomain.BlogKind(b.Kind), Repo: str(b.Repo), Branch: str(b.Branch), Dir: str(b.Dir),
		ContentDir: b.ContentDir, PublicDir: str(b.PublicDir), SiteURL: str(b.SiteUrl), PathTemplate: str(b.PathTemplate),
		DefaultLocale: str(b.DefaultLocale), Author: str(b.Author), Draft: b.Draft != nil && *b.Draft,
	}, Token: b.Token}
	if _, err := h.svc.SaveBlog(ctx, req.WorkspaceId, in); err != nil {
		return nil, err
	}
	out, err := h.integrations(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.SaveBlog200JSONResponse(out), nil
}

func (h *Handler) DeleteBlog(ctx context.Context, req gen.DeleteBlogRequestObject) (gen.DeleteBlogResponseObject, error) {
	if err := h.svc.DeleteBlog(ctx, req.WorkspaceId); err != nil {
		return nil, err
	}
	return gen.DeleteBlog204Response{}, nil
}

// YouTubeCallback finishes the OAuth flow and sends the browser back to
// the workspace settings with the outcome in the query string.
func (h *Handler) YouTubeCallback(ctx context.Context, req gen.YouTubeCallbackRequestObject) (gen.YouTubeCallbackResponseObject, error) {
	back := func(ws uuid.UUID, q url.Values) gen.YouTubeCallback302Response {
		loc := "/"
		if ws != uuid.Nil {
			loc = "/w/" + ws.String() + "/settings"
		}
		return gen.YouTubeCallback302Response{Headers: gen.YouTubeCallback302ResponseHeaders{Location: loc + "?" + q.Encode()}}
	}
	if req.Params.Error != nil {
		ws := h.svc.StateWorkspace(str(req.Params.State))
		return back(ws, url.Values{"youtube": {"error"}, "reason": {*req.Params.Error}}), nil
	}
	ws, err := h.svc.Callback(ctx, str(req.Params.State), str(req.Params.Code))
	if err != nil {
		return back(ws, url.Values{"youtube": {"error"}, "reason": {err.Error()}}), nil
	}
	return back(ws, url.Values{"youtube": {"connected"}}), nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
