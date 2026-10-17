package production

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kurgu/internal/apperror"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/pkg/archive"
	"kurgu/pkg/wav"
)

// A short cut from a long video speaks a slice of the long video's
// narration (docs/shorts.md §4): prepare puts the slice where `kurgu
// tool tts` would have, so the composition contract, the check and the
// render work unchanged.

// clipFade is how long the narration slice fades in and out.
const clipFade = 15 * time.Millisecond

// clipTail is how far a short's composition may run past its
// narration, for an end card (the check allows the same).
const clipTail = 4.0

// clipMaterial is what prepare tells the agent about a short's clip.
type clipMaterial struct {
	clip      storydomain.Clip
	sentences []Sentence // the slice's sentences, timed in the short
}

// prepareClip seeds dir for a short from its long video's latest
// revision: the narration slice and its timings, and its text; on the
// first draft the long video's media, credits and score; and always the
// long video's composition and scenes to read under sources/long/.
func (s *Service) prepareClip(ctx context.Context, st *storydomain.Story, d *storydomain.Deliverable, dir string) (clipMaterial, error) {
	clip := *d.Clip()
	out := clipMaterial{clip: clip}
	src, err := st.Deliverable(d.SourceID())
	if err != nil {
		return out, err
	}
	rev, err := s.stories.Revision(ctx, src.ID(), src.Revision())
	if err != nil {
		return out, err
	}
	key, ok := rev.Artifacts[storydomain.ArtifactWorkdir]
	if !ok {
		return out, apperror.New(apperror.NotFound, "the long video kept no working files to cut from")
	}
	long, err := os.MkdirTemp("", "kurgu-long-*")
	if err != nil {
		return out, err
	}
	defer func() { _ = os.RemoveAll(long) }()
	rc, _, err := s.blobs.Open(ctx, key)
	if err != nil {
		return out, fmt.Errorf("open the long video's work dir: %w", err)
	}
	err = archive.Untar(rc, long)
	_ = rc.Close()
	if err != nil {
		return out, fmt.Errorf("unpack the long video's work dir: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "narration"), 0o750); err != nil {
		return out, err
	}
	if err := cutNarration(filepath.Join(long, "narration", "speech.wav"), filepath.Join(dir, "narration", "speech.wav"), clip); err != nil {
		return out, err
	}
	raw, err := os.ReadFile(filepath.Join(long, narrationTimings)) //nolint:gosec // our temp dir
	if err != nil {
		return out, apperror.New(apperror.NotFound, "the long video kept no narration timings")
	}
	timings, sentences, err := sliceTimings(raw, clip)
	if err != nil {
		return out, err
	}
	out.sentences = sentences
	if err := os.WriteFile(filepath.Join(dir, narrationTimings), timings, 0o600); err != nil {
		return out, err
	}
	texts := make([]string, 0, len(sentences))
	for _, x := range sentences {
		texts = append(texts, x.Text)
	}
	if err := os.WriteFile(filepath.Join(dir, narrationFile), []byte(strings.Join(texts, " ")+"\n"), 0o600); err != nil {
		return out, err
	}

	if d.Revision() == 0 {
		if err := copyTree(filepath.Join(long, "assets"), filepath.Join(dir, "assets")); err != nil {
			return out, err
		}
		for _, name := range []string{"music.js", "music.json", "music.meta.json"} {
			if err := copyFile(filepath.Join(long, name), filepath.Join(dir, name)); err != nil {
				return out, err
			}
		}
	}
	_ = os.RemoveAll(filepath.Join(dir, "sources", "long"))
	for _, name := range []string{"composition.html", "scenes.json"} {
		if err := copyFile(filepath.Join(long, name), filepath.Join(dir, "sources", "long", name)); err != nil {
			return out, err
		}
	}
	return out, os.WriteFile(filepath.Join(dir, "sources", "clip.md"), []byte(clipDoc(clip, sentences)), 0o600)
}

// cutNarration writes the clip's span of the long narration in to out.
func cutNarration(in, out string, clip storydomain.Clip) error {
	f, err := os.Open(in) //nolint:gosec // our temp dir
	if err != nil {
		return apperror.New(apperror.NotFound, "the long video kept no narration audio")
	}
	defer func() { _ = f.Close() }()
	w, err := os.Create(out) //nolint:gosec // our work dir
	if err != nil {
		return err
	}
	if _, err := wav.Cut(f, w, clip.Start(), clip.End(), clipFade); err != nil {
		_ = w.Close()
		return fmt.Errorf("cut the long video's narration: %w", err)
	}
	return w.Close()
}

// sliceTimings keeps the words and sentences of the long narration's
// timings.json that fall within clip, shifted so the clip starts at 0,
// and the duration of the clip. Everything else in the file (engine,
// voice) is kept as it is.
func sliceTimings(raw []byte, clip storydomain.Clip) ([]byte, []Sentence, error) {
	var t map[string]json.RawMessage
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, nil, apperror.Wrap(apperror.InternalError, err, "the long video's narration timings are unreadable")
	}
	start, end := clip.Start().Seconds(), clip.End().Seconds()
	const slack = 0.02 // snapping and millisecond rounding
	within := func(a, b float64) bool { return a >= start-slack && b <= end+slack }
	shift := func(v float64) float64 { return math.Max(0, math.Round((v-start)*1000)/1000) }
	slice := func(name string) ([]map[string]any, error) {
		var items []map[string]any
		if r, ok := t[name]; ok {
			if err := json.Unmarshal(r, &items); err != nil {
				return nil, apperror.Wrap(apperror.InternalError, err, "the long video's narration timings are unreadable")
			}
		}
		var kept []map[string]any
		for _, it := range items {
			a, _ := it["start"].(float64)
			b, _ := it["end"].(float64)
			if within(a, b) {
				it["start"], it["end"] = shift(a), math.Min(shift(b), end-start)
				kept = append(kept, it)
			}
		}
		return kept, nil
	}
	words, err := slice("words")
	if err != nil {
		return nil, nil, err
	}
	segments, err := slice("segments")
	if err != nil {
		return nil, nil, err
	}
	if len(segments) == 0 {
		return nil, nil, apperror.New(apperror.ValidationFailed, "the clip holds no whole sentence of the long video's narration")
	}
	sentences := make([]Sentence, 0, len(segments))
	for _, seg := range segments {
		text, _ := seg["text"].(string)
		a, _ := seg["start"].(float64)
		b, _ := seg["end"].(float64)
		sentences = append(sentences, Sentence{Text: text, Start: seconds(a), End: seconds(b)})
	}
	set := func(name string, v any) {
		b, _ := json.Marshal(v)
		t[name] = b
	}
	set("words", nonNilSlice(words))
	set("segments", segments)
	set("duration", math.Round((end-start)*1000)/1000)
	b, err := json.MarshalIndent(t, "", "  ")
	return b, sentences, err
}

func nonNilSlice(s []map[string]any) []map[string]any {
	if s == nil {
		return []map[string]any{}
	}
	return s
}

// clipDoc tells the agent where the clip sits in the long video and
// what it says, sentence by sentence.
func clipDoc(clip storydomain.Clip, sentences []Sentence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# The clip\n\nThis short is %s to %s of the long video (%.1f s). "+
		"Its narration, sentence by sentence, timed in the short:\n\n",
		clock(clip.Start()), clock(clip.End()), clip.Length().Seconds())
	for _, x := range sentences {
		fmt.Fprintf(&b, "- %.2f–%.2f s: %s\n", x.Start.Seconds(), x.End.Seconds(), x.Text)
	}
	return b.String()
}

// clock writes t as m:ss.
func clock(t time.Duration) string {
	sec := int(t.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// copyFile copies a regular file; a missing source copies nothing.
func copyFile(from, to string) error {
	in, err := os.Open(from) //nolint:gosec // our temp dir
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		return err
	}
	out, err := os.Create(to) //nolint:gosec // our work dir
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// copyTree copies the regular files under from to to; a missing from
// copies nothing.
func copyTree(from, to string) error {
	if _, err := os.Stat(from); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(from, func(p string, e fs.DirEntry, err error) error {
		if err != nil || !e.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		return copyFile(p, filepath.Join(to, rel))
	})
}
