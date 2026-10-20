// Package httptransport assembles the HTTP server: the generated strict
// API under /api/v1 behind session auth and cross-origin protection,
// health checks, and the embedded web UI.
package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
	knowledgedomain "github.com/ahmethakanbesel/ekin/internal/domain/knowledge/v1"
	librarydomain "github.com/ahmethakanbesel/ekin/internal/domain/library/v1"
	"github.com/ahmethakanbesel/ekin/internal/gen"
	defaultssvc "github.com/ahmethakanbesel/ekin/internal/service/defaults/v1"
	identitysvc "github.com/ahmethakanbesel/ekin/internal/service/identity/v1"
	jobsvc "github.com/ahmethakanbesel/ekin/internal/service/job/v1"
	knowledgesvc "github.com/ahmethakanbesel/ekin/internal/service/knowledge/v1"
	librarysvc "github.com/ahmethakanbesel/ekin/internal/service/library/v1"
	pipelinesvc "github.com/ahmethakanbesel/ekin/internal/service/pipeline/v1"
	productionsvc "github.com/ahmethakanbesel/ekin/internal/service/production/v1"
	publishingsvc "github.com/ahmethakanbesel/ekin/internal/service/publishing/v1"
	seosvc "github.com/ahmethakanbesel/ekin/internal/service/seo/v1"
	templatesvc "github.com/ahmethakanbesel/ekin/internal/service/template/v1"
	voicesvc "github.com/ahmethakanbesel/ekin/internal/service/voice/v1"
	workspacesvc "github.com/ahmethakanbesel/ekin/internal/service/workspace/v1"
	defaultshandler "github.com/ahmethakanbesel/ekin/internal/transport/http/defaults/v1"
	identityhandler "github.com/ahmethakanbesel/ekin/internal/transport/http/identity/v1"
	jobhandler "github.com/ahmethakanbesel/ekin/internal/transport/http/job/v1"
	knowledgehandler "github.com/ahmethakanbesel/ekin/internal/transport/http/knowledge/v1"
	libraryhandler "github.com/ahmethakanbesel/ekin/internal/transport/http/library/v1"
	pipelinehandler "github.com/ahmethakanbesel/ekin/internal/transport/http/pipeline/v1"
	"github.com/ahmethakanbesel/ekin/internal/transport/http/principal"
	publishinghandler "github.com/ahmethakanbesel/ekin/internal/transport/http/publishing/v1"
	seohandler "github.com/ahmethakanbesel/ekin/internal/transport/http/seo/v1"
	storyhandler "github.com/ahmethakanbesel/ekin/internal/transport/http/story/v1"
	systemhandler "github.com/ahmethakanbesel/ekin/internal/transport/http/system/v1"
	templatehandler "github.com/ahmethakanbesel/ekin/internal/transport/http/template/v1"
	voicehandler "github.com/ahmethakanbesel/ekin/internal/transport/http/voice/v1"
	workerapi "github.com/ahmethakanbesel/ekin/internal/transport/http/worker"
	workspacehandler "github.com/ahmethakanbesel/ekin/internal/transport/http/workspace/v1"
)

// maxBodyBytes caps a request body; maxUploadBytes and
// maxLibraryUploadBytes cap document and library uploads (the file limit
// plus multipart overhead). libraryUploadTime is how long a library
// upload may take to arrive, past the server's read timeout.
const (
	maxBodyBytes          = 1 << 20
	maxUploadBytes        = knowledgedomain.MaxFileBytes + 1<<20
	maxLibraryUploadBytes = librarydomain.MaxUploadBytes + 1<<20
	libraryUploadTime     = 15 * time.Minute
)

// Deps is what the router needs.
type Deps struct {
	Identity   *identitysvc.Service
	Workspaces *workspacesvc.Service
	Pipelines  *pipelinesvc.Service
	Jobs       *jobsvc.Service
	Knowledge  *knowledgesvc.Service
	Library    *librarysvc.Service
	Production *productionsvc.Service
	Feed       storyhandler.Feed
	Inviter    identityhandler.Inviter
	System     *systemhandler.Handler
	Publishing *publishingsvc.Service
	SEO        *seosvc.Service
	Templates  *templatesvc.Service
	Voices     *voicesvc.Service
	Defaults   *defaultssvc.Service
	// WorkerToken enables the hybrid-worker API when set.
	WorkerToken string
	// Profiles serves remote workers their host profile.
	Profiles workerapi.Profiles
	// PublicURL is where browsers reach Ekin, e.g. https://ekin.example.com.
	// It prefixes invitation links, is trusted for cross-origin writes, and
	// an https scheme marks the session cookie Secure.
	PublicURL string
	// UI is the built web app; nil serves a "not built" hint.
	UI  fs.FS
	Log *slog.Logger
}

// Aliases give each embedded handler its own field name.
type (
	identities = identityhandler.Handler
	workspaces = workspacehandler.Handler
	pipelines  = pipelinehandler.Handler
	jobs       = jobhandler.Handler
	documents  = knowledgehandler.Handler
	library    = libraryhandler.Handler
	stories    = storyhandler.Handler
	system     = systemhandler.Handler
	publishing = publishinghandler.Handler
	seo        = seohandler.Handler
	templates  = templatehandler.Handler
	voices     = voicehandler.Handler
	defaults   = defaultshandler.Handler
)

// server stitches the per-context handlers into one StrictServerInterface.
type server struct {
	*identities
	*workspaces
	*pipelines
	*jobs
	*documents
	*library
	*stories
	*system
	*publishing
	*seo
	*templates
	*voices
	*defaults
}

var _ gen.StrictServerInterface = server{}

// NewRouter returns the root handler. It fails when PublicURL is not an
// absolute http(s) URL.
func NewRouter(d Deps) (http.Handler, error) {
	public, err := url.Parse(d.PublicURL)
	if err != nil || (public.Scheme != "http" && public.Scheme != "https") || public.Host == "" {
		return nil, fmt.Errorf("public URL %q must be an absolute http(s) URL", d.PublicURL)
	}
	csrf := http.NewCrossOriginProtection()
	if err := csrf.AddTrustedOrigin(public.Scheme + "://" + public.Host); err != nil {
		return nil, fmt.Errorf("trust public origin: %w", err)
	}
	cookies := principal.Cookies{Secure: public.Scheme == "https"}

	mux := http.NewServeMux()
	writeErr := func(w http.ResponseWriter, r *http.Request, err error) {
		writeError(w, r, d.Log, err)
	}
	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, r, apperror.New(apperror.Forbidden, "cross-origin request refused"))
	}))
	strict := gen.NewStrictHandlerWithOptions(server{
		identities: identityhandler.New(d.Identity, cookies, public.String(), d.Inviter),
		workspaces: workspacehandler.New(d.Workspaces),
		pipelines:  pipelinehandler.New(d.Pipelines),
		jobs:       jobhandler.New(d.Jobs),
		documents:  knowledgehandler.New(d.Knowledge),
		library:    libraryhandler.New(d.Library, storiesOf(d.Production)),
		stories:    storyhandler.New(d.Production, d.Feed),
		system:     d.System,
		publishing: publishinghandler.New(d.Publishing),
		seo:        seohandler.New(d.SEO),
		templates:  templatehandler.New(d.Templates),
		voices:     voicehandler.New(d.Voices),
		defaults:   defaultshandler.New(d.Defaults),
	}, []gen.StrictMiddlewareFunc{authenticate(d.Identity)}, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeErr(w, r, apperror.Wrap(apperror.BadRequest, err, "malformed request"))
		},
		ResponseErrorHandlerFunc: writeErr,
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeErr(w, r, apperror.Wrap(apperror.BadRequest, err, "invalid request parameters"))
		},
	})
	mux.Handle("GET /api/v1/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/revisions/{number}/{artifact}",
		media(d.Identity, d.Production, writeErr))
	mux.Handle("GET /api/v1/workspaces/{workspaceId}/library/assets/{assetId}/files/{file}",
		libraryFile(d.Identity, d.Library, writeErr))
	workerapi.Register(mux, d.WorkerToken, d.Jobs, d.Production, d.Profiles, d.Log, writeErr)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, r, apperror.New(apperror.NotFound, "no such endpoint"))
	}))
	mux.Handle("/", readOnly(spa(d.UI)))
	return recoverer(d.Log, securityHeaders(logRequests(d.Log, csrf.Handler(limitBody(mux))))), nil
}

// storiesOf names the stories library usages point at; nil without the
// production service (tests that leave it out).
func storiesOf(p *productionsvc.Service) libraryhandler.Stories {
	if p == nil {
		return nil
	}
	return p
}

// securityHeaders sets headers every response should carry. no-referrer
// keeps invitation tokens in /invite/... URLs from leaking onward.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// writeError renders err as {"code","message"} with the status its code
// maps to. Unclassified errors are logged and answered as a bare 500.
func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	code, ok := apperror.GetCode(err)
	message := apperror.Message(err)
	if !ok {
		code, message = apperror.InternalError, "internal error"
	}
	status := apperror.StatusFor(code)
	if status >= http.StatusInternalServerError {
		log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: string(code), Message: message})
}

// spa serves the built app, falling back to index.html for client-side
// routes.
func spa(ui fs.FS) http.Handler {
	if ui == nil {
		return notBuilt()
	}
	if _, err := fs.Stat(ui, "index.html"); err != nil {
		return notBuilt()
	}
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if _, err := fs.Stat(ui, name); err == nil {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, ui, "index.html")
	})
}

func notBuilt() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Web UI not built. Run `make web` and rebuild, or use `make dev`.\n"))
	})
}

func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxBodyBytes)
		upload := r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/workspaces/")
		if upload && strings.HasSuffix(r.URL.Path, "/documents") {
			limit = maxUploadBytes
		}
		if upload && strings.HasSuffix(r.URL.Path, "/library/assets") {
			limit = maxLibraryUploadBytes
			// A 300 MB video takes longer than the server's read timeout
			// on most uplinks.
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(libraryUploadTime))
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/worker/") {
			limit = workerapi.MaxUpload
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		// The mux records the matched pattern on r. Logging it instead of
		// the path keeps tokens (/invitations/{token}) out of the logs.
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		log.InfoContext(r.Context(), "request",
			slog.String("method", r.Method),
			slog.String("route", route),
			slog.Int("status", rec.status),
			slog.Duration("took", time.Since(start)))
	})
}

func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if errors.Is(asError(p), http.ErrAbortHandler) {
					panic(p)
				}
				log.ErrorContext(r.Context(), "handler panicked", slog.Any("panic", p), slog.String("path", r.URL.Path))
				writeError(w, r, log, apperror.New(apperror.InternalError, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func asError(p any) error {
	if err, ok := p.(error); ok {
		return err
	}
	return nil
}
