package render_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/internal/adapter/render"
	templatedomain "github.com/ahmethakanbesel/ekin/internal/domain/template/v1"
)

// Every font a template may pick must be bundled, with both the latin
// and the Turkish (latin-ext) subset, and every face must point at a
// file the kit ships.
func TestKitBundlesEveryTemplateFont(t *testing.T) {
	kit := render.Kit()
	css, err := fs.ReadFile(kit, "ekin.css")
	if err != nil {
		t.Fatal(err)
	}
	face := regexp.MustCompile(`@font-face \{ font-family: "([^"]+)";.*?src: url\(([^)]+)\)`)
	files := map[string][]string{}
	for _, m := range face.FindAllStringSubmatch(string(css), -1) {
		files[m[1]] = append(files[m[1]], m[2])
		if _, err := fs.Stat(kit, m[2]); err != nil {
			t.Errorf("%s: %s is missing from the kit", m[1], m[2])
		}
	}
	for _, font := range templatedomain.Fonts {
		var latin, ext bool
		for _, f := range files[font] {
			if strings.Contains(f, "-latin") {
				latin = true
			} else {
				ext = true
			}
		}
		if !latin || !ext {
			t.Errorf("%s: want latin and latin-ext faces in ekin.css, got %v", font, files[font])
		}
	}
}

