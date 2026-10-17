// Package remote is the hybrid worker's side of the worker API: it
// claims jobs from a Kurgu server over HTTP and moves working
// directories and renders back and forth.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"kurgu/internal/apperror"
	jobdomain "kurgu/internal/domain/job/v1"
	settingsdomain "kurgu/internal/domain/settings/v1"
	productionsvc "kurgu/internal/service/production/v1"
	workerapi "kurgu/internal/transport/http/worker"
	"kurgu/pkg/archive"
)

// Client talks to one server.
type Client struct {
	BaseURL string // https://kurgu.example.com
	Token   string
	Owner   string // this worker's name, e.g. mac-studio
	HTTP    *http.Client
}

func (c *Client) url(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + "/api/v1/worker" + path
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 0}
}

// post sends a JSON body; out may be nil. It returns the status code.
func (c *Client) post(ctx context.Context, path string, body, out any) (int, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) (int, error) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := c.client().Do(req)
	if err != nil {
		return 0, fmt.Errorf("remote: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		var e struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		_ = json.Unmarshal(raw, &e)
		msg := fmt.Sprintf("remote: %s answered %d %s: %s", req.URL.Path, res.StatusCode, e.Code, e.Message)
		if e.Code != "" {
			// Keep the server's code, so callers can tell a refusal (the
			// job is no longer ours) from a failure worth retrying.
			return res.StatusCode, apperror.New(apperror.Code(e.Code), msg)
		}
		return res.StatusCode, errors.New(msg)
	}
	if out != nil && res.StatusCode == http.StatusOK {
		return res.StatusCode, json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode, nil
}

// Profile fetches the host profile an admin saved on the server under
// name; ok is false when none was saved.
func (c *Client) Profile(ctx context.Context, name string) (p settingsdomain.Profile, ok bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/profiles/"+url.PathEscape(name)), nil)
	if err != nil {
		return p, false, err
	}
	code, err := c.do(req, &p)
	if code == http.StatusNotFound {
		return p, false, nil
	}
	return p, err == nil, err
}

// Claim implements jobsvc.Queue.
func (c *Client) Claim(ctx context.Context, queues []jobdomain.Queue, kinds []string, _ string, lease time.Duration) (*jobdomain.Job, error) {
	qs := make([]string, 0, len(queues))
	for _, q := range queues {
		qs = append(qs, string(q))
	}
	var snap jobdomain.Snapshot
	code, err := c.post(ctx, "/claim", workerapi.ClaimRequest{Owner: c.Owner, Queues: qs, Kinds: kinds, LeaseSeconds: int(lease.Seconds())}, &snap)
	if err != nil || code == http.StatusNoContent {
		return nil, err
	}
	return jobdomain.Rehydrate(snap)
}

// Heartbeat implements jobsvc.Queue.
func (c *Client) Heartbeat(ctx context.Context, jobID uuid.UUID, _ string, lease time.Duration) error {
	_, err := c.post(ctx, "/jobs/"+jobID.String()+"/heartbeat", workerapi.JobRequest{Owner: c.Owner, LeaseSeconds: int(lease.Seconds())}, nil)
	return err
}

// Complete implements jobsvc.Queue.
func (c *Client) Complete(ctx context.Context, jobID uuid.UUID, _ string, cause error) error {
	req := workerapi.JobRequest{Owner: c.Owner}
	if cause != nil {
		req.Error, req.Permanent = cause.Error(), errors.Is(cause, jobdomain.ErrPermanent)
	}
	_, err := c.post(ctx, "/jobs/"+jobID.String()+"/complete", req, nil)
	return err
}

// Release implements jobsvc.Queue.
func (c *Client) Release(ctx context.Context, jobID uuid.UUID, _ string) error {
	_, err := c.post(ctx, "/jobs/"+jobID.String()+"/release", workerapi.JobRequest{Owner: c.Owner}, nil)
	return err
}

// ExpireAbandoned implements jobsvc.Queue; the server sweeps its own.
func (c *Client) ExpireAbandoned(context.Context) (int, error) { return 0, nil }

// Fail records a failed step on the server.
func (c *Client) Fail(ctx context.Context, jobID uuid.UUID, cause error) error {
	_, err := c.post(ctx, "/jobs/"+jobID.String()+"/fail", workerapi.JobRequest{Owner: c.Owner, Error: cause.Error()}, nil)
	return err
}

// BeginDraft starts a draft on the server; ok is false when there is
// nothing to do.
func (c *Client) BeginDraft(ctx context.Context, jobID uuid.UUID) (productionsvc.DraftTask, bool, error) {
	var task productionsvc.DraftTask
	code, err := c.post(ctx, "/jobs/"+jobID.String()+"/draft/begin", workerapi.JobRequest{Owner: c.Owner}, &task)
	return task, err == nil && code == http.StatusOK, err
}

// BeginRender starts a render on the server.
func (c *Client) BeginRender(ctx context.Context, jobID uuid.UUID) (productionsvc.RenderTask, bool, error) {
	var task productionsvc.RenderTask
	code, err := c.post(ctx, "/jobs/"+jobID.String()+"/render/begin", workerapi.JobRequest{Owner: c.Owner}, &task)
	return task, err == nil && code == http.StatusOK, err
}

// FetchWorkdir replaces dir with the server's copy of the job's working
// directory.
func (c *Client) FetchWorkdir(ctx context.Context, jobID uuid.UUID, dir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/jobs/"+jobID.String()+"/workdir?owner="+c.Owner), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("remote: fetch work dir: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("remote: fetch work dir answered %d", res.StatusCode)
	}
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return archive.Untar(res.Body, dir)
}

// upload streams a multipart body built by write.
func (c *Client) upload(ctx context.Context, path string, write func(mw *multipart.Writer) error) error {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := write(mw)
		if err == nil {
			err = mw.Close()
		}
		_ = pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	_, err = c.do(req, nil)
	return err
}

func writeMeta(mw *multipart.Writer, meta any) error {
	w, err := mw.CreateFormField("meta")
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(meta)
}

// FinishDraft uploads the working directory and the agent's result.
func (c *Client) FinishDraft(ctx context.Context, jobID uuid.UUID, task productionsvc.DraftTask, res productionsvc.DraftResult, dir string) error {
	return c.upload(ctx, "/jobs/"+jobID.String()+"/draft/finish", func(mw *multipart.Writer) error {
		if err := writeMeta(mw, workerapi.DraftFinish{Owner: c.Owner, Task: task, Result: res}); err != nil {
			return err
		}
		w, err := mw.CreateFormFile("workdir", "workdir.tar.gz")
		if err != nil {
			return err
		}
		return archive.TarGz(dir, w)
	})
}

// FinishRender uploads rendered files (artifact name → local path).
func (c *Client) FinishRender(ctx context.Context, jobID uuid.UUID, task productionsvc.RenderTask, files map[string]string) error {
	return c.upload(ctx, "/jobs/"+jobID.String()+"/render/finish", func(mw *multipart.Writer) error {
		if err := writeMeta(mw, workerapi.RenderFinish{Owner: c.Owner, Task: task}); err != nil {
			return err
		}
		for name, path := range files {
			w, err := mw.CreateFormFile(name, name)
			if err != nil {
				return err
			}
			f, err := os.Open(path) //nolint:gosec // our own render output
			if err != nil {
				return err
			}
			_, err = io.Copy(w, f)
			_ = f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
}
