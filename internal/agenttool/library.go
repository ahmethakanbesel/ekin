package agenttool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	librarydomain "kurgu/internal/domain/library/v1"
	"kurgu/pkg/fold"
)

// CatalogPath is where a draft's library catalog lies in its working
// directory, written by the server before the agent starts.
const CatalogPath = "library/catalog.json"

// MusicTrackPath names the library track a video uses instead of a
// composed music.js.
const MusicTrackPath = "music.json"

// CatalogItem is one library asset a draft may use.
type CatalogItem struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Mood        string   `json:"mood,omitempty"`
	Tempo       int      `json:"tempo,omitempty"`
	Seconds     float64  `json:"seconds,omitempty"`
	Width       int      `json:"width,omitempty"`
	Height      int      `json:"height,omitempty"`
	Uses        int      `json:"uses"`
	// Format is the stored file's extension: webm, jpg, opus, or js for a
	// composed score.
	Format string `json:"format"`
	// Key is the file's blob key; SHA256 is the asset's content hash.
	Key    string               `json:"key"`
	SHA256 string               `json:"sha256"`
	Credit librarydomain.Credit `json:"credit,omitzero"`
}

// MusicTrack is music.json: a library track mixed under the voice from
// Offset seconds, looped when shorter than the video.
type MusicTrack struct {
	Asset  string  `json:"asset"`
	File   string  `json:"file"`
	Offset float64 `json:"offset,omitempty"`
}

// Fetcher copies a library file to a local path.
type Fetcher interface {
	Fetch(ctx context.Context, key, dest string) error
}

// ReadCatalog reads the draft's catalog; none means an empty one.
func ReadCatalog(dir string) ([]CatalogItem, error) {
	raw, err := os.ReadFile(filepath.Join(dir, CatalogPath)) //nolint:gosec // the agent's working directory
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []CatalogItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%s: %w", CatalogPath, err)
	}
	return items, nil
}

// ReadMusicTrack reads music.json, and false when there is none.
func ReadMusicTrack(dir string) (MusicTrack, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, MusicTrackPath)) //nolint:gosec // the agent's working directory
	if errors.Is(err, os.ErrNotExist) {
		return MusicTrack{}, false, nil
	}
	if err != nil {
		return MusicTrack{}, false, err
	}
	var t MusicTrack
	if err := json.Unmarshal(raw, &t); err != nil {
		return MusicTrack{}, true, fmt.Errorf("%s: %w", MusicTrackPath, err)
	}
	if t.File == "" || filepath.IsAbs(t.File) || strings.Contains(filepath.ToSlash(t.File), "..") {
		return t, true, fmt.Errorf("%s: file must be a path inside this directory", MusicTrackPath)
	}
	if t.Offset < 0 {
		return t, true, fmt.Errorf("%s: offset must not be negative", MusicTrackPath)
	}
	return t, true, nil
}

// LibraryIDs lists the library assets dir uses: those credited in
// assets/credits.json and the music track.
func LibraryIDs(dir string) []string {
	var ids []string
	for _, c := range readCredits(filepath.Join(dir, "assets", "credits.json")) {
		if c.Library != "" && !slices.Contains(ids, c.Library) {
			ids = append(ids, c.Library)
		}
	}
	if t, ok, err := ReadMusicTrack(dir); ok && err == nil && t.Asset != "" && !slices.Contains(ids, t.Asset) {
		ids = append(ids, t.Asset)
	}
	return ids
}

// maxListed bounds one library search's answer.
const maxListed = 20

// library runs `kurgu tool library`: search the catalog, or `use` an item.
func (e Env) library(ctx context.Context, args []string, kind, query string) error {
	items, err := ReadCatalog(e.Dir)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		if _, err := os.Stat(filepath.Join(e.Dir, CatalogPath)); err != nil {
			return errors.New("this video does not use the library; compose music and search stock as usual")
		}
		return errors.New("the library has nothing this video can use; compose or search where the instructions allow it, and do without where they do not")
	}
	if len(args) > 0 {
		if args[0] != "use" || len(args) != 2 {
			return errors.New("usage: kurgu tool library use <id>")
		}
		return e.useLibrary(ctx, items, args[1])
	}
	if kind != "" && kind != "music" && kind != "video" && kind != "photo" {
		return errors.New("--type must be music, video or photo")
	}
	words := strings.Fields(fold.String(query))
	type scored struct {
		item  CatalogItem
		score int
	}
	var hits []scored
	for _, it := range items {
		if kind != "" && it.Kind != kind {
			continue
		}
		text := fold.String(strings.Join(append([]string{it.Title, it.Description, it.Mood}, it.Tags...), " "))
		score := 0
		for _, w := range words {
			if strings.Contains(text, w) {
				score++
			}
		}
		if len(words) > 0 && score == 0 {
			continue
		}
		hits = append(hits, scored{it, score})
	}
	// Best matches first; the catalog's order (least used first) breaks ties.
	slices.SortStableFunc(hits, func(a, b scored) int { return b.score - a.score })
	if len(hits) == 0 {
		_, _ = fmt.Fprintln(e.Stdout, "nothing in the library matches; try other words, or no --query to see everything of this type")
		return nil
	}
	for i, h := range hits {
		if i == maxListed {
			_, _ = fmt.Fprintf(e.Stdout, "… and %d more; narrow it with --query\n", len(hits)-maxListed)
			break
		}
		_, _ = fmt.Fprintln(e.Stdout, describeItem(h.item))
	}
	_, _ = fmt.Fprintln(e.Stdout, "\nTake one with: kurgu tool library use <id>")
	return nil
}

func describeItem(it CatalogItem) string {
	var facts []string
	if it.Seconds > 0 {
		facts = append(facts, fmt.Sprintf("%.0f s", it.Seconds))
	}
	if it.Width > 0 {
		facts = append(facts, fmt.Sprintf("%dx%d", it.Width, it.Height))
	}
	if it.Mood != "" {
		facts = append(facts, it.Mood)
	}
	if it.Tempo > 0 {
		facts = append(facts, fmt.Sprintf("%d BPM", it.Tempo))
	}
	facts = append(facts, fmt.Sprintf("used %d×", it.Uses))
	line := fmt.Sprintf("%s  %s  %q (%s)", it.ID, it.Kind, it.Title, strings.Join(facts, ", "))
	if it.Description != "" {
		line += "\n    " + it.Description
	}
	if len(it.Tags) > 0 {
		line += "\n    tags: " + strings.Join(it.Tags, ", ")
	}
	return line
}

// useLibrary puts a catalog item in place and credits it.
func (e Env) useLibrary(ctx context.Context, items []CatalogItem, id string) error {
	i := slices.IndexFunc(items, func(it CatalogItem) bool { return it.ID == id })
	if i < 0 {
		return fmt.Errorf("%s is not in this video's library; list it with kurgu tool library", id)
	}
	it := items[i]
	if e.Library == nil {
		return errors.New("the library is not reachable from this machine")
	}
	var rel string
	switch {
	case it.Kind == "music" && it.Format == "js":
		rel = "music.js"
	default:
		rel = filepath.Join("assets", "library-"+it.ID+"."+it.Format)
	}
	if err := os.MkdirAll(filepath.Dir(e.path(rel)), 0o750); err != nil {
		return err
	}
	if err := e.Library.Fetch(ctx, it.Key, e.path(rel)); err != nil {
		return fmt.Errorf("fetch %s: %w", it.Title, err)
	}
	msg := rel
	// One music source at a time: a track replaces a composed score and a
	// library score replaces a track.
	switch {
	case it.Kind == "music" && it.Format == "js":
		_ = os.Remove(e.path(MusicTrackPath))
		if e.Spec.Media.AllowsFresh(librarydomain.KindMusic) {
			msg += " (a composed score from the library; you may adapt it, then run kurgu tool music)"
		} else {
			msg += " (a composed score from the library; use it as it is and run kurgu tool music)"
		}
	case it.Kind == "music":
		removed := os.Remove(e.path("music.js")) == nil
		b, _ := json.MarshalIndent(MusicTrack{Asset: it.ID, File: rel}, "", "  ")
		if err := os.WriteFile(e.path(MusicTrackPath), b, 0o600); err != nil {
			return err
		}
		msg += fmt.Sprintf(" and %s: mixed under the voice, looped if shorter than the video", MusicTrackPath)
		msg += "; set \"offset\" there (seconds) to start later in the track"
		if removed {
			msg += "\nmusic.js was removed: the track replaces it"
		}
	case it.Kind == "video":
		msg += fmt.Sprintf(" (%dx%d, %.0f s, loop it with Kurgu.video(el).seek(t))", it.Width, it.Height, it.Seconds)
	default:
		msg += fmt.Sprintf(" (%dx%d)", it.Width, it.Height)
	}
	credits := readCredits(e.path("assets/credits.json"))
	credits = slices.DeleteFunc(credits, func(c Credit) bool { return c.Library == it.ID })
	credits = append(credits, Credit{File: filepath.ToSlash(rel), Source: "Library", Kind: it.Kind, Library: it.ID,
		Author: it.Credit.Author, URL: it.Credit.URL, Licence: it.Credit.Licence, Seconds: int(it.Seconds)})
	if err := writeCredits(e.path("assets"), credits); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(e.Stdout, msg)
	if !it.Credit.IsZero() {
		_, _ = fmt.Fprintln(e.Stdout, "credit it in the description: "+creditLine(it.Credit))
	}
	return nil
}

func creditLine(c librarydomain.Credit) string {
	parts := []string{}
	for _, s := range []string{c.Author, c.Licence, c.URL} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// fileSHA256 hashes a file, "" when it cannot be read.
func fileSHA256(p string) string {
	f, err := os.Open(p) //nolint:gosec // the agent's working directory
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CheckMedia enforces the spec's media policy on dir: no Pexels stock of
// a library-only kind, no composed score when music is library-only, one
// music source, and library files that are present. A library item need
// not be in this draft's catalog when its file is there: a short arrives
// with its long video's media, and an item may be archived since.
func CheckMedia(dir string, spec Spec) []string {
	var problems []string
	items, err := ReadCatalog(dir)
	if err != nil {
		return []string{err.Error()}
	}
	inCatalog := func(id string) bool {
		return slices.ContainsFunc(items, func(it CatalogItem) bool { return it.ID == id })
	}
	for _, c := range readCredits(filepath.Join(dir, "assets", "credits.json")) {
		kind := c.Kind
		if kind == "" && c.Library == "" {
			kind = kindFromFile(c.File)
		}
		if c.Library == "" && !spec.Media.AllowsFresh(librarydomain.Kind(kind)) && referenced(dir, c.File) {
			problems = append(problems, fmt.Sprintf("%s comes from Pexels, but this video takes %ss from the library only; replace it with kurgu tool library --type %s", c.File, kind, kind))
		}
		if c.Library != "" && !inCatalog(c.Library) && !exists(dir, c.File) {
			problems = append(problems, fmt.Sprintf("%s names library item %s, which is neither in this video's library nor here", c.File, c.Library))
		}
	}
	track, hasTrack, err := ReadMusicTrack(dir)
	if err != nil {
		problems = append(problems, err.Error())
	}
	_, scoreErr := os.Stat(filepath.Join(dir, "music.js"))
	hasScore := scoreErr == nil
	switch {
	case hasTrack && hasScore:
		problems = append(problems, "music.js and music.json both exist; keep one music source")
	case hasTrack && err == nil:
		if !exists(dir, track.File) {
			problems = append(problems, "music.json names "+track.File+", which is missing; run kurgu tool library use again")
		}
	case hasScore && !spec.Media.AllowsFresh(librarydomain.KindMusic):
		sum := fileSHA256(filepath.Join(dir, "music.js"))
		if !slices.ContainsFunc(items, func(it CatalogItem) bool { return it.Format == "js" && it.SHA256 == sum }) {
			problems = append(problems, "this video takes its music from the library only; replace music.js with a track from kurgu tool library --type music")
		}
	}
	return problems
}

func exists(dir, file string) bool {
	_, err := os.Stat(filepath.Join(dir, file))
	return err == nil
}

// referenced reports whether composition.html names file; a download the
// composition does not use breaks no rule.
func referenced(dir, file string) bool {
	src, err := os.ReadFile(filepath.Join(dir, "composition.html")) //nolint:gosec // the agent's working directory
	if err != nil {
		return true
	}
	return strings.Contains(string(src), filepath.Base(file))
}

// kindFromFile tells a stock file's kind from its name (pexels-video-1.webm).
func kindFromFile(file string) string {
	switch {
	case strings.Contains(file, "-video-") || strings.HasSuffix(file, ".webm"):
		return "video"
	case strings.Contains(file, "-photo-") || strings.HasSuffix(file, ".jpg"):
		return "photo"
	}
	return ""
}
