package httptransport

import (
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	identitydomain "kurgu/internal/domain/identity/v1"
	identitysvc "kurgu/internal/service/identity/v1"
	productionsvc "kurgu/internal/service/production/v1"
	"kurgu/internal/transport/http/principal"
)

// artifactTypes are the artifact names served and their media types.
var artifactTypes = map[string]string{
	".mp4":  "video/mp4",
	".jpg":  "image/jpeg",
	".wav":  "audio/wav",
	".md":   "text/markdown; charset=utf-8",
	".json": "application/json",
	".gz":   "application/gzip",
}

// media streams a revision's artifact. It lives outside the strict
// server because video players need Range requests, which the generated
// binary responses do not support. Access is the same as the spec's
// view operations.
func media(identity *identitysvc.Service, stories *productionsvc.Service, writeErr func(http.ResponseWriter, *http.Request, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(principal.CookieName)
		if err != nil {
			writeErr(w, r, apperror.New(apperror.Unauthenticated, "not signed in"))
			return
		}
		user, _, err := identity.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		ids := make([]uuid.UUID, 3)
		for i, name := range []string{"workspaceId", "storyId", "deliverableId"} {
			if ids[i], err = uuid.Parse(r.PathValue(name)); err != nil {
				writeErr(w, r, apperror.New(apperror.BadRequest, "invalid "+name))
				return
			}
		}
		if _, err := identity.Authorize(r.Context(), user.ID(), ids[0], identitydomain.ActionView); err != nil {
			writeErr(w, r, err)
			return
		}
		number, err := strconv.Atoi(r.PathValue("number"))
		if err != nil || number < 1 {
			writeErr(w, r, apperror.New(apperror.BadRequest, "invalid revision number"))
			return
		}
		name := r.PathValue("artifact")
		contentType, ok := artifactTypes[strings.ToLower(path.Ext(name))]
		if !ok || strings.Contains(name, "/") {
			writeErr(w, r, apperror.New(apperror.NotFound, "no such artifact"))
			return
		}
		rc, size, err := stories.Artifact(r.Context(), ids[0], ids[1], ids[2], number, name)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		defer func() { _ = rc.Close() }()
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		if r.URL.Query().Get("download") != "" {
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		}
		if rs, ok := rc.(io.ReadSeeker); ok {
			http.ServeContent(w, r, name, time.Time{}, rs)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		_, _ = io.Copy(w, rc)
	})
}
