package agenttool_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ekin/internal/adapter/render"
	"ekin/internal/agenttool"
)

// drawer pretends to draw a thumbnail page: each variant shows text[n]
// and puts corner[n] in the bottom-right corner.
type drawer struct {
	meta     render.Meta
	text     map[int]string
	corner   map[int]string
	problems []string
	err      error
}

func (d drawer) Thumbnails(_ context.Context, _, _ string, ns []int, small bool, outDir string) ([]render.ThumbnailShot, render.Meta, []string, error) {
	if d.err != nil {
		return nil, d.meta, d.problems, d.err
	}
	var shots []render.ThumbnailShot
	for _, n := range ns {
		s := render.ThumbnailShot{N: n, Text: d.text[n], Path: filepath.Join(outDir, "x.jpg"), Bytes: 1}
		if c := d.corner[n]; c != "" {
			s.Corner = []string{c}
		}
		if small {
			s.Small = filepath.Join(outDir, "x.small.jpg")
		}
		shots = append(shots, s)
	}
	return shots, d.meta, d.problems, nil
}

func TestCheckThumbnails(t *testing.T) {
	const page = `<script src="/kit/ekin.js"></script><script>Ekin.thumbnails({render(v) {}})</script>`
	const two = `{"recommended": 1, "variants": [{"n": 1, "concept": "type", "text": "Kaygı mı?"}, {"n": 2, "concept": "frame", "text": "3 fark"}]}`
	hd := render.Meta{Width: 1280, Height: 720, FPS: 1, Duration: 1}
	good := drawer{meta: hd, text: map[int]string{1: "KAYGI MI?", 2: "Tam 3 fark\nvar"}}
	tests := []struct {
		name       string
		page, json string
		want, only int
		d          drawer
		errs       []string
		files      int
	}{
		{name: "every variant", page: page, json: two, want: 2, d: good, files: 4},
		{name: "one variant", page: page, json: two, want: 2, only: 2, d: good, files: 2},
		{name: "no page", json: two, want: 2, d: good, errs: []string{"thumbnail.html is missing"}},
		{name: "no json", page: page, want: 2, d: good, errs: []string{"thumbnails.json is missing"}},
		{name: "network", page: page + `<img src="https://example.com/a.jpg">`, json: two, want: 2, d: good, errs: []string{"references the network"}},
		{name: "count", page: page, json: two, want: 3, d: good, errs: []string{"needs 3"}},
		{name: "missing variant", page: page, json: two, want: 2, only: 4, d: good, errs: []string{"no variant 4"}},
		{name: "size", page: page, json: two, want: 2, d: drawer{meta: render.Meta{Width: 1080, Height: 1920}, text: good.text}, errs: []string{"1280x720"}, files: 4},
		{name: "text typed into the page", page: page, json: two, want: 2,
			d: drawer{meta: hd, text: map[int]string{1: "Kaygı mı?", 2: "Üç fark"}}, errs: []string{`thumbnail 2 does not show its text "3 fark"`}, files: 4},
		{name: "corner", page: page, json: two, want: 2,
			d: drawer{meta: hd, text: good.text, corner: map[int]string{1: "acme"}}, errs: []string{"thumbnail 1 puts text in the bottom-right corner"}, files: 4},
		{name: "page problems", page: page, json: two, want: 2,
			d: drawer{meta: hd, problems: []string{"exception: boom"}, err: errors.New("render: thumbnail 1: boom")}, errs: []string{"page: exception: boom", "render: thumbnail 1: boom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.page != "" {
				_ = os.WriteFile(filepath.Join(dir, agenttool.ThumbnailPage), []byte(tt.page), 0o600)
			}
			if tt.json != "" {
				_ = os.WriteFile(filepath.Join(dir, agenttool.ThumbnailsFile), []byte(tt.json), 0o600)
			}
			rep := agenttool.CheckThumbnails(context.Background(), tt.d, dir, tt.want, tt.only, true)
			if rep.OK != (len(tt.errs) == 0) || len(rep.Errors) != len(tt.errs) {
				t.Fatalf("report = %+v, want errors %v", rep, tt.errs)
			}
			for i, want := range tt.errs {
				if !strings.Contains(rep.Errors[i], want) {
					t.Errorf("error %d = %q, want it to mention %q", i, rep.Errors[i], want)
				}
			}
			if len(rep.Files) != tt.files {
				t.Errorf("files = %v, want %d", rep.Files, tt.files)
			}
		})
	}
}

func TestThumbnailToolNeedsVariants(t *testing.T) {
	var out strings.Builder
	e := agenttool.Env{Dir: t.TempDir(), Stdout: &out, Stderr: &out}
	if err := agenttool.Run(context.Background(), e, []string{"thumbnail"}); err == nil || !strings.Contains(err.Error(), "no thumbnail variants") {
		t.Fatalf("thumbnail without variants = %v", err)
	}
	e.Spec.Thumbnails = 2
	if err := agenttool.Run(context.Background(), e, []string{"thumbnail", "--stills", "1,x"}); err == nil || !strings.Contains(err.Error(), "--stills") {
		t.Fatalf("bad stills = %v", err)
	}
}
