// Package worker serves the hybrid-worker API: a worker on another host
// (a Mac with a GPU, say) claims heavy jobs from this server, downloads
// the job's working directory, runs the agent or the renderer locally,
// and uploads the result. It authenticates with KURGU_WORKER_TOKEN and
// never touches the database or blob store directly.
//
// The routes live outside the OpenAPI spec because they stream archives
// and media both ways.
package worker

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	storydomain "kurgu/internal/domain/story/v1"
	jobsvc "kurgu/internal/service/job/v1"
	productionsvc "kurgu/internal/service/production/v1"
	"kurgu/pkg/archive"
)

// MaxUpload bounds a finish upload (a long final render).
const MaxUpload = 8 << 30

// ClaimRequest asks for the next job.
type ClaimRequest struct {
	Owner        string   `json:"owner"`
	Queues       []string `json:"queues"`
	Kinds        []string `json:"kinds"`
	LeaseSeconds int      `json:"leaseSeconds"`
}

// JobRequest addresses a claimed job.
type JobRequest struct {
	Owner        string `json:"owner"`
	LeaseSeconds int    `json:"leaseSeconds,omitempty"`
	Error        string `json:"error,omitempty"`
	Permanent    bool   `json:"permanent,omitempty"`
}

// DraftFinish is the metadata part of a draft upload.
type DraftFinish struct {
	Owner  string                    `json:"owner"`
	Task   productionsvc.DraftTask   `json:"task"`
	Result productionsvc.DraftResult `json:"result"`
}

// RenderFinish is the metadata part of a render upload.
type RenderFinish struct {
	Owner string                   `json:"owner"`
	Task  productionsvc.RenderTask `json:"task"`
}

// API serves the routes.
type API struct {
	token      string
	jobs       *jobsvc.Service
	production *productionsvc.Service
	log        *slog.Logger
	writeErr   func(http.ResponseWriter, *http.Request, error)
}

// Register adds the worker routes to mux. With an empty token the API is
// off.
func Register(mux *http.ServeMux, token string, jobs *jobsvc.Service, production *productionsvc.Service, log *slog.Logger,
	writeErr func(http.ResponseWriter, *http.Request, error)) {
	if token == "" {
		return
	}
	a := &API{token: token, jobs: jobs, production: production, log: log, writeErr: writeErr}
	mux.Handle("POST /api/v1/worker/claim", a.auth(a.claim))
	mux.Handle("POST /api/v1/worker/jobs/{id}/heartbeat", a.auth(a.heartbeat))
	mux.Handle("POST /api/v1/worker/jobs/{id}/complete", a.auth(a.complete))
	mux.Handle("POST /api/v1/worker/jobs/{id}/release", a.auth(a.release))
	mux.Handle("POST /api/v1/worker/jobs/{id}/fail", a.auth(a.fail))
	mux.Handle("GET /api/v1/worker/jobs/{id}/workdir", a.auth(a.workdir))
	mux.Handle("POST /api/v1/worker/jobs/{id}/draft/begin", a.auth(a.beginDraft))
	mux.Handle("POST /api/v1/worker/jobs/{id}/draft/finish", a.auth(a.finishDraft))
	mux.Handle("POST /api/v1/worker/jobs/{id}/render/begin", a.auth(a.beginRender))
	mux.Handle("POST /api/v1/worker/jobs/{id}/render/finish", a.auth(a.finishRender))
}

func (a *API) auth(next func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) != 1 {
			a.writeErr(w, r, apperror.New(apperror.Unauthenticated, "bad worker token"))
			return
		}
		if err := next(w, r); err != nil {
			a.writeErr(w, r, err)
		}
	})
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return apperror.Wrap(apperror.BadRequest, err, "malformed body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

func lease(seconds int) time.Duration {
	if seconds <= 0 {
		return 2 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}

func (a *API) claim(w http.ResponseWriter, r *http.Request) error {
	var req ClaimRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.Owner == "" {
		return apperror.New(apperror.ValidationFailed, "owner is required")
	}
	queues := make([]jobdomain.Queue, 0, len(req.Queues))
	for _, q := range req.Queues {
		pq, err := jobdomain.ParseQueue(q)
		if err != nil {
			return err
		}
		queues = append(queues, pq)
	}
	j, err := a.jobs.Claim(r.Context(), queues, req.Kinds, "remote:"+req.Owner, lease(req.LeaseSeconds))
	if err != nil {
		return err
	}
	if j == nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	return writeJSON(w, j.Snapshot())
}

// held returns the job when owner holds its lease.
func (a *API) held(r *http.Request, owner string) (*jobdomain.Job, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, apperror.New(apperror.BadRequest, "invalid job id")
	}
	j, err := a.jobs.ByID(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if j.Status() != jobdomain.StatusRunning || j.LeaseOwner() != "remote:"+owner {
		return nil, apperror.New(apperror.Conflict, "this worker does not hold the job")
	}
	return j, nil
}

func (a *API) heartbeat(w http.ResponseWriter, r *http.Request) error {
	var req JobRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	id, _ := uuid.Parse(r.PathValue("id"))
	if err := a.jobs.Heartbeat(r.Context(), id, "remote:"+req.Owner, lease(req.LeaseSeconds)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) complete(w http.ResponseWriter, r *http.Request) error {
	var req JobRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	id, _ := uuid.Parse(r.PathValue("id"))
	var cause error
	if req.Error != "" {
		cause = errors.New(req.Error)
		if req.Permanent {
			cause = jobdomain.Permanent(cause)
		}
	}
	if err := a.jobs.Complete(r.Context(), id, "remote:"+req.Owner, cause); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) release(w http.ResponseWriter, r *http.Request) error {
	var req JobRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	id, _ := uuid.Parse(r.PathValue("id"))
	if err := a.jobs.Release(r.Context(), id, "remote:"+req.Owner); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func payloadOf(j *jobdomain.Job) (productionsvc.DeliverablePayload, error) {
	var p productionsvc.DeliverablePayload
	if err := json.Unmarshal(j.Payload(), &p); err != nil {
		return p, apperror.Wrap(apperror.BadRequest, err, "job is not a deliverable job")
	}
	return p, nil
}

// fail records a remote step failure on the deliverable.
func (a *API) fail(w http.ResponseWriter, r *http.Request) error {
	var req JobRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	j, err := a.held(r, req.Owner)
	if err != nil {
		return err
	}
	p, err := payloadOf(j)
	if err != nil {
		return err
	}
	a.production.FailStep(r.Context(), p, errors.New(req.Error))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) workdir(w http.ResponseWriter, r *http.Request) error {
	j, err := a.held(r, r.URL.Query().Get("owner"))
	if err != nil {
		return err
	}
	p, err := payloadOf(j)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/gzip")
	return archive.TarGz(a.production.WorkDir(p.DeliverableID), w)
}

func (a *API) beginDraft(w http.ResponseWriter, r *http.Request) error {
	var req JobRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	j, err := a.held(r, req.Owner)
	if err != nil {
		return err
	}
	p, err := payloadOf(j)
	if err != nil {
		return err
	}
	task, dir, err := a.production.BeginDraft(r.Context(), p)
	if err != nil {
		return err
	}
	if dir == "" {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	return writeJSON(w, task)
}

// replaceDir swaps dir's contents for the uploaded archive.
func replaceDir(dir string, body io.Reader) error {
	tmp := dir + ".upload"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return err
	}
	if err := archive.Untar(body, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return apperror.Wrap(apperror.BadRequest, err, "bad work dir archive")
	}
	_ = os.RemoveAll(dir)
	return os.Rename(tmp, dir)
}

func (a *API) multipart(r *http.Request) (*multipart.Reader, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, MaxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, apperror.Wrap(apperror.BadRequest, err, "expected multipart")
	}
	return mr, nil
}

func (a *API) finishDraft(w http.ResponseWriter, r *http.Request) error {
	mr, err := a.multipart(r)
	if err != nil {
		return err
	}
	var meta DraftFinish
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return apperror.New(apperror.BadRequest, "missing workdir part")
		}
		if err != nil {
			return apperror.Wrap(apperror.BadRequest, err, "malformed upload")
		}
		switch part.FormName() {
		case "meta":
			if err := json.NewDecoder(io.LimitReader(part, 1<<20)).Decode(&meta); err != nil {
				return apperror.Wrap(apperror.BadRequest, err, "malformed meta")
			}
		case "workdir":
			j, err := a.held(r, meta.Owner)
			if err != nil {
				return err
			}
			p, err := payloadOf(j)
			if err != nil {
				return err
			}
			if meta.Task.Payload != p {
				return apperror.New(apperror.BadRequest, "task does not match the job")
			}
			dir := a.production.WorkDir(p.DeliverableID)
			if err := replaceDir(dir, part); err != nil {
				return err
			}
			if err := a.production.FinishDraft(r.Context(), meta.Task, dir, meta.Result); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}
	}
}

func (a *API) beginRender(w http.ResponseWriter, r *http.Request) error {
	var req JobRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	j, err := a.held(r, req.Owner)
	if err != nil {
		return err
	}
	p, err := payloadOf(j)
	if err != nil {
		return err
	}
	task, dir, err := a.production.BeginRender(r.Context(), p)
	if err != nil {
		return err
	}
	if dir == "" {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	return writeJSON(w, task)
}

var renderArtifacts = map[string]bool{
	storydomain.ArtifactPreview: true, storydomain.ArtifactFinal: true, storydomain.ArtifactPoster: true,
}

func (a *API) finishRender(w http.ResponseWriter, r *http.Request) error {
	mr, err := a.multipart(r)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "kurgu-remote-render-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	var meta RenderFinish
	files := map[string]string{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return apperror.Wrap(apperror.BadRequest, err, "malformed upload")
		}
		name := part.FormName()
		switch {
		case name == "meta":
			if err := json.NewDecoder(io.LimitReader(part, 1<<20)).Decode(&meta); err != nil {
				return apperror.Wrap(apperror.BadRequest, err, "malformed meta")
			}
		case renderArtifacts[name]:
			path := filepath.Join(tmp, name)
			f, err := os.Create(path) //nolint:gosec // fixed names in a temp dir
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, part); err != nil {
				_ = f.Close()
				return apperror.Wrap(apperror.BadRequest, err, "upload interrupted")
			}
			if err := f.Close(); err != nil {
				return err
			}
			files[name] = path
		}
	}
	j, err := a.held(r, meta.Owner)
	if err != nil {
		return err
	}
	p, err := payloadOf(j)
	if err != nil {
		return err
	}
	if meta.Task.Payload != p {
		return apperror.New(apperror.BadRequest, "task does not match the job")
	}
	if err := a.production.FinishRender(r.Context(), meta.Task, files); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
