package production

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	"github.com/ahmethakanbesel/ekin/pkg/mp4"
)

// markdownNoise is what a reader never sees: link targets, images and
// HTML comments.
var markdownNoise = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)|\]\([^)]*\)|<!--[\s\S]*?-->`)

// wordsIn counts the words a deliverable says or shows in dir: a
// video's narration, an article's body. 0 when the file is missing.
func wordsIn(d *storydomain.Deliverable, dir string) int {
	if d.IsVideo() {
		b, err := os.ReadFile(filepath.Join(dir, narrationFile)) //nolint:gosec // our own work dir
		if err != nil {
			return 0
		}
		return countWords(string(b))
	}
	b, err := os.ReadFile(filepath.Join(dir, articleFile)) //nolint:gosec // our own work dir
	if err != nil {
		return 0
	}
	a, _ := ParseArticle(b)
	if a.Body == "" {
		return 0
	}
	return countWords(stripMarkdownNoise(a.Body))
}

// narrationTimings is where `ekin tool tts` says what it read and who
// read it.
const narrationTimings = "narration/timings.json"

// narratorIn reads which voice read a video's narration in dir; zero
// for an article or a video never narrated.
func narratorIn(d *storydomain.Deliverable, dir string) storydomain.Narrator {
	var n storydomain.Narrator
	if !d.IsVideo() {
		return n
	}
	b, err := os.ReadFile(filepath.Join(dir, narrationTimings)) //nolint:gosec // our own work dir
	if err != nil || json.Unmarshal(b, &n) != nil {
		return storydomain.Narrator{}
	}
	return n
}

// stripMarkdownNoise keeps a link's text and drops its target, images
// and comments.
func stripMarkdownNoise(md string) string {
	return markdownNoise.ReplaceAllStringFunc(md, func(m string) string {
		if strings.HasPrefix(m, "]") {
			return "]"
		}
		return " "
	})
}

// countWords counts the space-separated runs that hold a letter or a
// digit, so Markdown marks and dashes standing alone are not words.
func countWords(text string) int {
	n := 0
	for _, f := range strings.Fields(text) {
		if strings.IndexFunc(f, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			n++
		}
	}
	return n
}

// videoLength reads a rendered video's length; 0 when it cannot.
func videoLength(path string) time.Duration {
	f, err := os.Open(path) //nolint:gosec // our own render output
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	d, err := mp4.Duration(f)
	if err != nil {
		return 0
	}
	return d
}
