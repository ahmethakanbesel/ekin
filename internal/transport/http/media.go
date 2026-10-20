package httptransport

import (
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	identitydomain "github.com/ahmethakanbesel/ekin/internal/domain/identity/v1"
	librarydomain "github.com/ahmethakanbesel/ekin/internal/domain/library/v1"
	identitysvc "github.com/ahmethakanbesel/ekin/internal/service/identity/v1"
	librarysvc "github.com/ahmethakanbesel/ekin/internal/service/library/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	"github.com/ahmethakanbesel/ekin/internal/transport/http/principal"
)

// artifactTypes are the artifact names served and their media types.
var artifactTypes = map[string]string{
	".mp4":  "video/mp4",
	".jpg":  "image/jpeg",
	".wav":  "audio/wav",
	".md":   "text/markdown; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".json": "application/json",
	".gz":   "application/gzip",
	".srt":  "application/x-subrip; charset=utf-8",
	".vtt":  "text/vtt; charset=utf-8",
	".zip":  "application/zip",
}

// media streams a revision's artifact. It lives outside the strict
// server because video players need Range requests, which the generated
// binary responses do not support. Access is the same as the spec's
// view operations.
func media(identity *identitysvc.Service, stories *productionsvc.Service, writeErr func(http.ResponseWriter, *http.Request, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids, err := viewer(identity, r, "workspaceId", "storyId", "deliverableId")
		if err != nil {
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
		w.Header().Set("Content-Type", contentType)
		if r.URL.Query().Get("download") != "" {
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		}
		serve(w, r, name, rc, size)
	})
}

// libraryFile streams a library asset's main file or poster, outside the
// strict server for the same Range reason as media.
func libraryFile(identity *identitysvc.Service, library *librarysvc.Service, writeErr func(http.ResponseWriter, *http.Request, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids, err := viewer(identity, r, "workspaceId", "assetId")
		if err != nil {
			writeErr(w, r, err)
			return
		}
		f, err := librarydomain.ParseFile(r.PathValue("file"))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		a, rc, size, err := library.Open(r.Context(), ids[0], ids[1], f)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		w.Header().Set("Content-Type", a.ContentType(f))
		serve(w, r, string(f), rc, size)
	})
}

// viewer authenticates the session cookie and checks that its user may
// view the workspace named by the first of the uuid path values, which
// it returns parsed.
func viewer(identity *identitysvc.Service, r *http.Request, names ...string) ([]uuid.UUID, error) {
	cookie, err := r.Cookie(principal.CookieName)
	if err != nil {
		return nil, apperror.New(apperror.Unauthenticated, "not signed in")
	}
	user, _, err := identity.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(names))
	for i, name := range names {
		if ids[i], err = uuid.Parse(r.PathValue(name)); err != nil {
			return nil, apperror.New(apperror.BadRequest, "invalid "+name)
		}
	}
	if _, err := identity.Authorize(r.Context(), user.ID(), ids[0], identitydomain.ActionView); err != nil {
		return nil, err
	}
	return ids, nil
}

// serve copies a blob to the response, with Range support when the blob
// can seek, and closes it.
func serve(w http.ResponseWriter, r *http.Request, name string, rc io.ReadCloser, size int64) {
	defer func() { _ = rc.Close() }()
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, time.Time{}, rs)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, rc)
}
