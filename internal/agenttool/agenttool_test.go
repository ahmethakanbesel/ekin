package agenttool_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kurgu/internal/adapter/render"
	"kurgu/internal/agenttool"
)

func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestCheck(t *testing.T) {
	if testing.Short() || render.FindChrome() == "" {
		t.Skip("needs Chrome")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("needs ffmpeg")
	}
	dir := copyDir(t, "../adapter/render/testdata/comp")
	spec := agenttool.Spec{Language: "tr", Width: 1080, Height: 1920, MinDuration: 1, MaxDuration: 60}
	r := &render.Renderer{}

	rep := agenttool.Check(context.Background(), r, dir, "composition.html", spec)
	if rep.OK || !strings.Contains(strings.Join(rep.Errors, "\n"), "metadata.json is missing") {
		t.Fatalf("missing metadata passed: %+v", rep)
	}
	_ = os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"title":"Kaygı ve endişe","description":"x","tags":["kaygı"]}`), 0o600)
	rep = agenttool.Check(context.Background(), r, dir, "composition.html", spec)
	if !rep.OK {
		t.Fatalf("valid composition failed: %+v", rep)
	}

	spec.Width, spec.Height = 1920, 1080
	if rep := agenttool.Check(context.Background(), r, dir, "composition.html", spec); rep.OK {
		t.Fatal("wrong orientation passed")
	}
	spec.Width, spec.Height = 1080, 1920
	page, _ := os.ReadFile(filepath.Join(dir, "composition.html"))
	_ = os.WriteFile(filepath.Join(dir, "composition.html"), bytes.Replace(page, []byte("</div>\n<script>"),
		[]byte(`<img src="https://images.example.com/a.jpg"></div><script>`), 1), 0o600)
	rep = agenttool.Check(context.Background(), r, dir, "composition.html", spec)
	if rep.OK || !strings.Contains(strings.Join(rep.Errors, "\n"), "references the network") {
		t.Fatalf("network reference passed: %+v", rep)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	var out bytes.Buffer
	env := agenttool.Env{Dir: t.TempDir(), Stdout: &out, Stderr: &out}
	for _, args := range [][]string{{}, {"nope"}, {"frame", "--at", "x"}, {"stock"}, {"tts"}} {
		if err := agenttool.Run(context.Background(), env, args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}
