// Package httptransport assembles the HTTP server: the generated strict
// API under /api/v1, health checks, and the embedded web UI.
package httptransport

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"kurgu/internal/apperror"
	"kurgu/internal/gen"
	jobsvc "kurgu/internal/service/job/v1"
	pipelinesvc "kurgu/internal/service/pipeline/v1"
	workspacesvc "kurgu/internal/service/workspace/v1"
	jobhandler "kurgu/internal/transport/http/job/v1"
	pipelinehandler "kurgu/internal/transport/http/pipeline/v1"
	workspacehandler "kurgu/internal/transport/http/workspace/v1"
)

// maxBodyBytes caps a request body. Uploads will get their own route.
const maxBodyBytes = 1 << 20

// Deps is what the router needs.
type Deps struct {
	Workspaces *workspacesvc.Service
	Pipelines  *pipelinesvc.Service
	Jobs       *jobsvc.Service
	// UI is the built web app; nil serves a "not built" hint.
	UI  fs.FS
	Log *slog.Logger
}

// Aliases give each embedded handler its own field name.
type (
	workspaces = workspacehandler.Handler
	pipelines  = pipelinehandler.Handler
	jobs       = jobhandler.Handler
)

// server stitches the per-context handlers into one StrictServerInterface.
type server struct {
	*workspaces
	*pipelines
	*jobs
}

var _ gen.StrictServerInterface = server{}

// NewRouter returns the root handler.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()
	writeErr := func(w http.ResponseWriter, r *http.Request, err error) {
		writeError(w, r, d.Log, err)
	}
	strict := gen.NewStrictHandlerWithOptions(server{
		workspaces: workspacehandler.New(d.Workspaces),
		pipelines:  pipelinehandler.New(d.Pipelines),
		jobs:       jobhandler.New(d.Jobs),
	}, nil, gen.StrictHTTPServerOptions{
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
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, r, apperror.New(apperror.NotFound, "no such endpoint"))
	}))
	mux.Handle("/", readOnly(spa(d.UI)))
	return recoverer(d.Log, logRequests(d.Log, limitBody(mux)))
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
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
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
		log.InfoContext(r.Context(), "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
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
