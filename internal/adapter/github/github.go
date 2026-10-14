// Package github commits files to a repository in one commit through the
// Git Data API (blobs → tree → commit → ref), so an article and its
// images land together and trigger one site build.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client calls the GitHub REST API.
type Client struct {
	BaseURL string // default https://api.github.com
	HTTP    *http.Client
}

// File is one file to write.
type File struct {
	Path    string
	Content []byte
}

// Commit writes files on branch of repo ("owner/name") with message and
// returns the commit SHA.
func (c *Client) Commit(ctx context.Context, token, repo, branch, message string, files []File) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do(ctx, token, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+branch, nil, &ref); err != nil {
		return "", err
	}
	var parent struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.do(ctx, token, http.MethodGet, "/repos/"+repo+"/git/commits/"+ref.Object.SHA, nil, &parent); err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	type entry struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	tree := make([]entry, 0, len(files))
	for _, f := range files {
		var blob struct {
			SHA string `json:"sha"`
		}
		body := map[string]string{"content": base64.StdEncoding.EncodeToString(f.Content), "encoding": "base64"}
		if err := c.do(ctx, token, http.MethodPost, "/repos/"+repo+"/git/blobs", body, &blob); err != nil {
			return "", err
		}
		tree = append(tree, entry{Path: strings.TrimPrefix(f.Path, "/"), Mode: "100644", Type: "blob", SHA: blob.SHA})
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
		map[string]any{"message": message, "tree": newTree.SHA, "parents": []string{ref.Object.SHA}}, &commit); err != nil {
		return "", err
	}
	if err := c.do(ctx, token, http.MethodPatch, "/repos/"+repo+"/git/refs/heads/"+branch,
		map[string]any{"sha": commit.SHA, "force": false}, nil); err != nil {
		return "", err
	}
	return commit.SHA, nil
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
		return fmt.Errorf("github: %s %s answered %d: %s", method, path, res.StatusCode, e.Message)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
