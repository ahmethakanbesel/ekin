// Package github commits files to a repository in one commit through the
// Git Data API (blobs → tree → commit → ref), so an article and its
// images land together and trigger one site build. The commit goes onto
// a branch directly, or onto a branch of its own with a pull request.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Client calls the GitHub REST API.
type Client struct {
	BaseURL string // default https://api.github.com
	HTTP    *http.Client
}

// File is one file to write, or to delete.
type File struct {
	Path    string
	Content []byte
	// Executable writes the file with mode 100755.
	Executable bool
	// Delete removes the file instead of writing it.
	Delete bool
}

// Commit writes files on branch of repo ("owner/name") with message and
// returns the commit SHA.
func (c *Client) Commit(ctx context.Context, token, repo, branch, message string, files []File) (string, error) {
	head, err := c.head(ctx, token, repo, branch)
	if err != nil {
		return "", err
	}
	sha, err := c.commitOnto(ctx, token, repo, head, message, files)
	if err != nil {
		return "", err
	}
	if err := c.do(ctx, token, http.MethodPatch, "/repos/"+repo+"/git/refs/heads/"+branch,
		map[string]any{"sha": sha, "force": false}, nil); err != nil {
		return "", err
	}
	return sha, nil
}

// PullRequest is a pull request Ekin opened.
type PullRequest struct {
	Number int
	URL    string // the pull request's page
	Branch string
}

// PullRequestInput is what OpenPullRequest writes and asks to merge.
type PullRequestInput struct {
	Repo   string // owner/name
	Base   string // the branch to merge into
	Branch string // the new branch; Ekin owns it
	Title  string // also the commit message
	Body   string
	Files  []File
	// From is the commit the branch starts from; empty for the tip of
	// Base. A change starts from the commit it was made on, so the pull
	// request shows exactly its edits and any conflict with later work.
	From string
}

// OpenPullRequest commits in.Files onto the tip of in.Base, points
// in.Branch at that commit and opens a pull request from it. It is safe
// to call again with the same input after an attempt cut short: an open
// pull request from in.Branch is returned as it is, and a branch left
// behind without one is moved to a fresh commit.
func (c *Client) OpenPullRequest(ctx context.Context, token string, in PullRequestInput) (PullRequest, error) {
	owner, _, ok := strings.Cut(in.Repo, "/")
	if !ok || in.Base == "" || in.Branch == "" || in.Branch == in.Base {
		return PullRequest{}, errors.New("github: a repository, a base branch and a different head branch are required")
	}
	var open []pullJSON
	q := url.Values{"head": {owner + ":" + in.Branch}, "base": {in.Base}, "state": {"open"}}
	if err := c.do(ctx, token, http.MethodGet, "/repos/"+in.Repo+"/pulls?"+q.Encode(), nil, &open); err != nil {
		return PullRequest{}, err
	}
	if len(open) > 0 {
		return PullRequest{Number: open[0].Number, URL: open[0].HTMLURL, Branch: in.Branch}, nil
	}
	base := in.From
	if base == "" {
		var err error
		if base, err = c.head(ctx, token, in.Repo, in.Base); err != nil {
			return PullRequest{}, err
		}
	}
	sha, err := c.commitOnto(ctx, token, in.Repo, base, in.Title, in.Files)
	if err != nil {
		return PullRequest{}, err
	}
	err = c.do(ctx, token, http.MethodPost, "/repos/"+in.Repo+"/git/refs",
		map[string]any{"ref": "refs/heads/" + in.Branch, "sha": sha}, nil)
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
		// The branch is left from an attempt that stopped before the
		// pull request; it is Ekin's, so it moves to the new commit.
		err = c.do(ctx, token, http.MethodPatch, "/repos/"+in.Repo+"/git/refs/heads/"+in.Branch,
			map[string]any{"sha": sha, "force": true}, nil)
	}
	if err != nil {
		return PullRequest{}, err
	}
	var pr pullJSON
	if err := c.do(ctx, token, http.MethodPost, "/repos/"+in.Repo+"/pulls",
		map[string]any{"title": in.Title, "head": in.Branch, "base": in.Base, "body": in.Body}, &pr); err != nil {
		return PullRequest{}, err
	}
	return PullRequest{Number: pr.Number, URL: pr.HTMLURL, Branch: in.Branch}, nil
}

// PullState is where a pull request stands.
type PullState string

const (
	PullOpen   PullState = "open"
	PullMerged PullState = "merged"
	PullClosed PullState = "closed" // closed without merging
)

// PullRequestState reports whether pull request number of repo is open,
// merged or closed without merging.
func (c *Client) PullRequestState(ctx context.Context, token, repo string, number int) (PullState, error) {
	var pr pullJSON
	if err := c.do(ctx, token, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), nil, &pr); err != nil {
		return "", err
	}
	switch {
	case pr.Merged:
		return PullMerged, nil
	case pr.State == "closed":
		return PullClosed, nil
	default:
		return PullOpen, nil
	}
}

type pullJSON struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
}

// Head returns the SHA branch points at.
func (c *Client) Head(ctx context.Context, token, repo, branch string) (string, error) {
	return c.head(ctx, token, repo, branch)
}

// ChangedFiles lists the paths that differ between commits base and
// head of repo (at most the 300 GitHub's compare reports).
func (c *Client) ChangedFiles(ctx context.Context, token, repo, base, head string) ([]string, error) {
	if base == head {
		return nil, nil
	}
	var cmp struct {
		Files []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
		} `json:"files"`
	}
	if err := c.do(ctx, token, http.MethodGet, "/repos/"+repo+"/compare/"+base+"..."+head, nil, &cmp); err != nil {
		return nil, err
	}
	var out []string
	for _, f := range cmp.Files {
		out = append(out, f.Filename)
		if f.PreviousFilename != "" {
			out = append(out, f.PreviousFilename)
		}
	}
	return out, nil
}

// Tarball streams repo at commit ref as a gzipped tar. Every entry sits
// under one top-level directory, which the caller strips.
func (c *Client) Tarball(ctx context.Context, token, repo, ref string) (io.ReadCloser, error) {
	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	path := "/repos/" + repo + "/tarball/" + url.PathEscape(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	// GitHub redirects to a signed download URL on another host; Go
	// drops the Authorization header across hosts, which that URL
	// does not need.
	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: GET %s: %w", path, err)
	}
	if res.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
		_ = res.Body.Close()
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		return nil, &Error{Method: http.MethodGet, Path: path, Status: res.StatusCode, Message: e.Message}
	}
	return res.Body, nil
}

// head returns the SHA branch points at.
func (c *Client) head(ctx context.Context, token, repo, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do(ctx, token, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+branch, nil, &ref); err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}

// commitOnto writes files in one commit whose parent is parentSHA and
// returns it; no branch moves.
func (c *Client) commitOnto(ctx context.Context, token, repo, parentSHA, message string, files []File) (string, error) {
	var parent struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.do(ctx, token, http.MethodGet, "/repos/"+repo+"/git/commits/"+parentSHA, nil, &parent); err != nil {
		return "", err
	}
	files = append([]File(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	type entry struct {
		Path string  `json:"path"`
		Mode string  `json:"mode"`
		Type string  `json:"type"`
		SHA  *string `json:"sha"` // null deletes the path
	}
	tree := make([]entry, 0, len(files))
	for _, f := range files {
		path := strings.TrimPrefix(f.Path, "/")
		if f.Delete {
			tree = append(tree, entry{Path: path, Mode: "100644", Type: "blob"})
			continue
		}
		var blob struct {
			SHA string `json:"sha"`
		}
		body := map[string]string{"content": base64.StdEncoding.EncodeToString(f.Content), "encoding": "base64"}
		if err := c.do(ctx, token, http.MethodPost, "/repos/"+repo+"/git/blobs", body, &blob); err != nil {
			return "", err
		}
		mode := "100644"
		if f.Executable {
			mode = "100755"
		}
		tree = append(tree, entry{Path: path, Mode: mode, Type: "blob", SHA: &blob.SHA})
	}
	var newTree struct {
		SHA string `json:"sha"`
	}
	if err := c.do(ctx, token, http.MethodPost, "/repos/"+repo+"/git/trees",
		map[string]any{"base_tree": parent.Tree.SHA, "tree": tree}, &newTree); err != nil {
		return "", err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := c.do(ctx, token, http.MethodPost, "/repos/"+repo+"/git/commits",
		map[string]any{"message": message, "tree": newTree.SHA, "parents": []string{parentSHA}}, &commit); err != nil {
		return "", err
	}
	return commit.SHA, nil
}

// Error is an answer from GitHub outside 2xx.
type Error struct {
	Method, Path string
	Status       int
	Message      string
}

func (e *Error) Error() string {
	return fmt.Sprintf("github: %s %s answered %d: %s", e.Method, e.Path, e.Status, e.Message)
}

func (c *Client) do(ctx context.Context, token, method, path string, body, out any) error {
	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		p, _, _ := strings.Cut(path, "?")
		return &Error{Method: method, Path: p, Status: res.StatusCode, Message: e.Message}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
