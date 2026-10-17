package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ekin/internal/adapter/render"
	storydomain "ekin/internal/domain/story/v1"
	"ekin/pkg/fold"
)

// The thumbnail contract (docs/thumbnails.md): one static page draws
// every variant listed in thumbnails.json.
const (
	ThumbnailPage   = "thumbnail.html"
	ThumbnailsFile  = "thumbnails.json"
	ThumbnailsDir   = "thumbnails"
	thumbnailWidth  = 1280
	thumbnailHeight = 720
	maxStills       = 8
)

// ThumbnailDrawer draws a thumbnail page's variants (render.Renderer).
type ThumbnailDrawer interface {
	Thumbnails(ctx context.Context, dir, pagePath string, ns []int, small bool, outDir string) ([]render.ThumbnailShot, render.Meta, []string, error)
}

// ThumbnailReport is the result of `ekin tool thumbnail`.
type ThumbnailReport struct {
	OK     bool     `json:"ok"`
	Files  []string `json:"files,omitempty"`
	Errors []string `json:"errors"`
}

// CheckThumbnails parses thumbnails.json (want variants; 0 takes any
// count), draws the variants (only one when only > 0) into
// dir/thumbnails/ and checks each: the page loads cleanly from this
// directory, is 1280×720, shows the variant's words, and leaves the
// corner YouTube covers with the duration empty. small also writes the
// feed-size copies.
func CheckThumbnails(ctx context.Context, r ThumbnailDrawer, dir string, want, only int, small bool) ThumbnailReport {
	rep := ThumbnailReport{Errors: []string{}}
	fail := func(format string, a ...any) { rep.Errors = append(rep.Errors, fmt.Sprintf(format, a...)) }
	src, err := os.ReadFile(filepath.Join(dir, ThumbnailPage)) //nolint:gosec // the agent's own file
	if err != nil {
		fail("%s is missing; write it with Ekin.thumbnails (see KIT.md)", ThumbnailPage)
		return rep
	}
	if externalRef.Match(src) {
		fail("%s references the network; use files in this directory", ThumbnailPage)
	}
	if !strings.Contains(string(src), "/kit/ekin.js") {
		fail("%s must load /kit/ekin.js and call Ekin.thumbnails", ThumbnailPage)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ThumbnailsFile)) //nolint:gosec // the agent's own file
	if err != nil {
		fail("%s is missing", ThumbnailsFile)
		return rep
	}
	thumbs, err := storydomain.ParseThumbnails(raw, want)
	if err != nil {
		fail("%s", strings.TrimPrefix(err.Error(), "ValidationFailed: "))
		return rep
	}
	if len(rep.Errors) > 0 {
		return rep
	}
	var ns []int
	for _, v := range thumbs.Variants {
		if only == 0 || v.N == only {
			ns = append(ns, v.N)
		}
	}
	if len(ns) == 0 {
		fail("%s has no variant %d", ThumbnailsFile, only)
		return rep
	}
	shots, m, problems, err := r.Thumbnails(ctx, dir, ThumbnailPage, ns, small, filepath.Join(dir, ThumbnailsDir))
	for _, p := range problems {
		fail("page: %s", p)
	}
	if err != nil {
		fail("%v", err)
		return rep
	}
	if m.Width != thumbnailWidth || m.Height != thumbnailHeight {
		fail("the page declares %dx%d; thumbnails are %dx%d", m.Width, m.Height, thumbnailWidth, thumbnailHeight)
	}
	for _, s := range shots {
		v := variant(thumbs, s.N)
		if !strings.Contains(fold.String(s.Text), fold.String(v.Text)) {
			fail("thumbnail %d does not show its text %q from %s; take the words from variant.text", s.N, v.Text, ThumbnailsFile)
		}
		if len(s.Corner) > 0 {
			fail("thumbnail %d puts text in the bottom-right corner, where YouTube shows the duration: %q", s.N, strings.Join(s.Corner, " "))
		}
		for _, p := range []string{s.Path, s.Small} {
			if rel, err := filepath.Rel(dir, p); err == nil && p != "" {
				rep.Files = append(rep.Files, rel)
			}
		}
	}
	rep.OK = len(rep.Errors) == 0
	return rep
}

func variant(t storydomain.Thumbnails, n int) storydomain.Thumbnail {
	for _, v := range t.Variants {
		if v.N == n {
			return v
		}
	}
	return storydomain.Thumbnail{}
}

// thumbnail runs `ekin tool thumbnail`: with stills, renders those
// composition times at full size into thumbnails/stills/<t>.jpg for the
// page to use; otherwise draws and checks the variants.
func (e Env) thumbnail(ctx context.Context, stills string, only int) error {
	if e.Spec.Thumbnails == 0 {
		return errors.New("this video has no thumbnail variants: it keeps a frame of the video as its thumbnail")
	}
	if stills != "" {
		return e.stills(ctx, stills)
	}
	rep := CheckThumbnails(ctx, &e.Renderer, e.Dir, e.Spec.Thumbnails, only, true)
	b, _ := json.MarshalIndent(rep, "", "  ")
	_, _ = fmt.Fprintln(e.Stdout, string(b))
	if !rep.OK {
		return errors.New("thumbnail check failed: fix every error above and run ekin tool thumbnail again")
	}
	_, _ = fmt.Fprintln(e.Stdout, "Read the .small.jpg files: that is how a phone's feed shows them. Fix anything you cannot read at that size.")
	return nil
}

func (e Env) stills(ctx context.Context, list string) error {
	var times []float64
	for part := range strings.SplitSeq(list, ",") {
		t, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || t < 0 {
			return fmt.Errorf("--stills: %q is not a number of seconds", part)
		}
		times = append(times, t)
	}
	if len(times) > maxStills {
		return fmt.Errorf("--stills: at most %d at a time", maxStills)
	}
	out := e.path(filepath.Join(ThumbnailsDir, "stills"))
	paths, problems, err := e.Renderer.Snapshot(ctx, e.Dir, "composition.html", times, 1, out)
	for _, p := range problems {
		_, _ = fmt.Fprintln(e.Stdout, "PROBLEM: "+p)
	}
	if err != nil {
		return err
	}
	for i, p := range paths {
		name := filepath.Join(out, StillName(times[i]))
		if err := os.Rename(p, name); err != nil {
			return err
		}
		rel, _ := filepath.Rel(e.Dir, name)
		_, _ = fmt.Fprintln(e.Stdout, rel)
	}
	return nil
}

// StillName is the file a still at t seconds is written to.
func StillName(t float64) string {
	return strconv.FormatFloat(t, 'f', -1, 64) + ".jpg"
}
