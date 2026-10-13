package httptransport

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	identitydomain "kurgu/internal/domain/identity/v1"
	"kurgu/internal/gen"
	identitysvc "kurgu/internal/service/identity/v1"
	"kurgu/internal/transport/http/principal"
)

// workspaceActions maps every operation under /workspaces/{workspaceId}
// to the action it needs. An operation missing here is refused (and a
// test fails), so a new endpoint cannot ship unguarded.
var workspaceActions = map[string]identitydomain.Action{
	"GetWorkspace":    identitydomain.ActionView,
	"UpdateWorkspace": identitydomain.ActionManage,

	"ListMembers":      identitydomain.ActionView,
	"ChangeMemberRole": identitydomain.ActionManage,
	// Leaving is open to every member; removing others is checked by
	// the domain (Membership.CheckRemoval).
	"RemoveMember":     identitydomain.ActionView,
	"ListInvitations":  identitydomain.ActionManage,
	"CreateInvitation": identitydomain.ActionManage,
	"RevokeInvitation": identitydomain.ActionManage,

	"ListPipelines":    identitydomain.ActionView,
	"GetPipeline":      identitydomain.ActionView,
	"CreatePipeline":   identitydomain.ActionEdit,
	"RedefinePipeline": identitydomain.ActionEdit,
	"DeletePipeline":   identitydomain.ActionEdit,
	"PausePipeline":    identitydomain.ActionEdit,
	"ResumePipeline":   identitydomain.ActionEdit,
	"RunPipeline":      identitydomain.ActionEdit,

	"ListJobs":  identitydomain.ActionView,
	"GetJob":    identitydomain.ActionView,
	"CancelJob": identitydomain.ActionEdit,
}

// authenticate is the strict-server middleware that enforces the spec's
// security: operations marked `security: []` pass through; every other
// one needs a live session, and workspace operations also need a
// membership whose role allows the operation's action.
func authenticate(identity *identitysvc.Service) gen.StrictMiddlewareFunc {
	return func(next gen.StrictHandlerFunc, operation string) gen.StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			// The generated wrapper sets SessionScopes only on operations
			// whose spec requires the session scheme.
			if ctx.Value(gen.SessionScopes) == nil {
				return next(ctx, w, r, req)
			}
			cookie, err := r.Cookie(principal.CookieName)
			if err != nil {
				return nil, apperror.New(apperror.Unauthenticated, "not signed in")
			}
			user, session, err := identity.Authenticate(ctx, cookie.Value)
			if err != nil {
				return nil, err
			}
			p := principal.Principal{User: user, Session: session}
			if raw := r.PathValue("workspaceId"); raw != "" {
				action, ok := workspaceActions[operation]
				if !ok {
					return nil, apperror.New(apperror.InternalError, "operation "+operation+" has no access rule")
				}
				workspaceID, err := uuid.Parse(raw)
				if err != nil {
					return nil, apperror.Wrap(apperror.BadRequest, err, "invalid workspace id")
				}
				if p.Membership, err = identity.Authorize(ctx, user.ID(), workspaceID, action); err != nil {
					return nil, err
				}
			}
			return next(principal.With(ctx, p), w, r, req)
		}
	}
}
