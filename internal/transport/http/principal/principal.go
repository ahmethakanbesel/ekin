// Package principal carries the authenticated caller through a request's
// context and builds the session cookie. The router's auth middleware
// fills it in; handlers read it.
package principal

import (
	"context"
	"net/http"
	"time"

	"ekin/internal/apperror"
	identitydomain "ekin/internal/domain/identity/v1"
)

// CookieName is the session cookie (api/openapi.yaml securitySchemes).
const CookieName = "ekin_session"

// Principal is who is calling and, on workspace routes, their membership.
type Principal struct {
	User    *identitydomain.User
	Session *identitydomain.Session
	// Membership is set on /workspaces/{workspaceId}/... routes.
	Membership *identitydomain.Membership
}

type key struct{}

// With returns ctx carrying p.
func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, key{}, p)
}

// From returns the caller. Handlers of authenticated operations can rely
// on it; the error only fires if a route skipped the middleware.
func From(ctx context.Context) (Principal, error) {
	p, ok := ctx.Value(key{}).(Principal)
	if !ok || p.User == nil {
		return Principal{}, apperror.New(apperror.Unauthenticated, "not signed in")
	}
	return p, nil
}

// Member returns the caller's membership in the route's workspace.
func Member(ctx context.Context) (*identitydomain.Membership, error) {
	p, err := From(ctx)
	if err != nil {
		return nil, err
	}
	if p.Membership == nil {
		return nil, apperror.New(apperror.InternalError, "route has no workspace membership")
	}
	return p.Membership, nil
}

// Cookies builds Set-Cookie values. Secure is on whenever the public URL
// is https.
type Cookies struct {
	Secure bool
}

// Session returns the cookie for a new session. It lives as long as the
// session's hard lifetime; the server enforces the idle timeout.
func (c Cookies) Session(token string, s *identitydomain.Session) string {
	return c.cookie(token, s.CreatedAt().Add(identitydomain.SessionLifetime))
}

// Clear returns a cookie that deletes the session cookie.
func (c Cookies) Clear() string {
	return c.cookie("", time.Unix(0, 0))
}

func (c Cookies) cookie(value string, expires time.Time) string {
	ck := &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires.UTC(),
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
	}
	if value == "" {
		ck.MaxAge = -1
	}
	return ck.String()
}
