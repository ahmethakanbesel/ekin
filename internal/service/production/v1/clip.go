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

	"github.com/ahmethakanbesel/ekin/internal/agenttool"
	"github.com/ahmethakanbesel/ekin/internal/apperror"
	storydomain "github.com/ahmethakanbesel/ekin/internal/domain/story/v1"
	"github.com/ahmethakanbesel/ekin/pkg/archive"
	"github.com/ahmethakanbesel/ekin/pkg/wav"
)

// A short cut from a long video speaks a slice of the long video's
// narration (docs/shorts.md §4): prepare puts the slice where `ekin
// tool tts` would have, so the composition contract, the check and the
// render work unchanged; clip/ holds a window around it the agent may
// move the cut into with `ekin tool clip`.

// clipMaterial is what prepare tells the agent about a short's clip.
type clipMaterial struct {
	clip            storydomain.Clip
	window          agenttool.Clip
	windowSentences []agenttool.Sentence
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
	long, err := os.MkdirTemp("", "ekin-long-*")
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

	raw, err := os.ReadFile(filepath.Join(long, narrationTimings)) //nolint:gosec // our temp dir
	if err != nil {
		return out, apperror.New(apperror.NotFound, "the long video kept no narration timings")
	}
	c, err := s.cutClip(st, d, filepath.Join(long, "narration", "speech.wav"), raw, dir)
	if err != nil {
		return out, err
	}
	out.window = c
	if out.windowSentences, err = agenttool.WindowSentences(dir); err != nil {
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
	return out, os.WriteFile(filepath.Join(dir, "sources", "clip.md"), []byte(clipDoc(out)), 0o600)
}

// movedClip is the clip a short's draft moved its cut to with `ekin
// tool clip`; nil when the cut stayed (or d is not a short with a clip).
func movedClip(d *storydomain.Deliverable, dir string) (*storydomain.Clip, error) {
	if d.Clip() == nil {
		return nil, nil
	}
	c, ok, err := agenttool.ReadClip(dir)
	if err != nil || !ok {
		return nil, err
	}
	ms := func(s float64) time.Duration { return time.Duration(math.Round(s*1000)) * time.Millisecond }
	start, end := ms(c.WindowStart+c.Start), ms(c.WindowStart+c.End)
	near := func(a, b time.Duration) bool { return (a - b).Abs() <= 2*time.Millisecond }
	if near(start, d.Clip().Start()) && near(end, d.Clip().End()) {
		return nil, nil
	}
	clip, err := storydomain.NewClip(start, end)
	if err != nil {
		return nil, err
	}
	return &clip, nil
}

// clipWindow is how much of the long narration around the clip the
// agent may move the cut into.
const clipWindow = 15.0

// cutClip writes the window of the long narration around d's clip to
// clip/, the clip itself to narration/ and where both sit to
// clip/clip.json.
func (s *Service) cutClip(st *storydomain.Story, d *storydomain.Deliverable, speech string, timings []byte, dir string) (agenttool.Clip, error) {
	var t struct {
		Duration float64 `json:"duration"`
	}
	_ = json.Unmarshal(timings, &t)
	c0, c1 := d.Clip().Start().Seconds(), d.Clip().End().Seconds()
	w0, w1 := math.Max(0, c0-clipWindow), c1+clipWindow
	if t.Duration > 0 {
		w1 = math.Min(w1, t.Duration)
	}
	window, _, err := agenttool.SliceTimings(timings, w0, w1)
	if err != nil {
		return agenttool.Clip{}, apperror.Wrap(apperror.ValidationFailed, err, "the long video's narration has no sentences around the clip")
	}
	if err := os.MkdirAll(filepath.Join(dir, "clip"), 0o750); err != nil {
		return agenttool.Clip{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, agenttool.ClipWindowJSON), window, 0o600); err != nil {
		return agenttool.Clip{}, err
	}
	if err := cutWAV(speech, filepath.Join(dir, agenttool.ClipWindowWAV), w0, w1); err != nil {
		return agenttool.Clip{}, err
	}
	if _, err := agenttool.CutNarration(filepath.Join(dir, agenttool.ClipWindowWAV), window, c0-w0, c1-w0, dir); err != nil {
		return agenttool.Clip{}, apperror.Wrap(apperror.ValidationFailed, err, "cut the short's narration")
	}
	l := s.cfg.Shorts
	c := agenttool.Clip{
		WindowStart: w0, Start: c0 - w0, End: c1 - w0,
		MinLength: l.ClipMin().Seconds(), MaxLength: l.ClipMax().Seconds(), Earliest: 0, Latest: w1 - w0,
	}
	for _, m := range st.Moments(d.SourceID()) {
		if m.Status() != storydomain.MomentMade || m.ShortID() == d.ID() {
			continue
		}
		gap := l.MinGap().Seconds()
		if e := m.Clip().End().Seconds(); e <= c0 {
			c.Earliest = math.Max(c.Earliest, e+gap-w0)
		}
		if b := m.Clip().Start().Seconds(); b >= c1 {
			c.Latest = math.Min(c.Latest, b-gap-w0)
		}
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return c, os.WriteFile(filepath.Join(dir, agenttool.ClipFile), b, 0o600)
}

// cutWAV writes start–end seconds of the WAV in to out.
func cutWAV(in, out string, start, end float64) error {
	f, err := os.Open(in) //nolint:gosec // our temp dir
	if err != nil {
		return apperror.New(apperror.NotFound, "the long video kept no narration audio")
	}
	defer func() { _ = f.Close() }()
	w, err := os.Create(out) //nolint:gosec // our work dir
	if err != nil {
		return err
	}
	sec := func(s float64) time.Duration { return time.Duration(math.Round(s*1000)) * time.Millisecond }
	if _, err := wav.Cut(f, w, sec(start), sec(end), agenttool.ClipFade); err != nil {
		_ = w.Close()
		return fmt.Errorf("cut the long video's narration: %w", err)
	}
	return w.Close()
}

// clipDoc tells the agent where the clip sits in the long video, what
// it says, and the window of sentences around it it may move into.
func clipDoc(m clipMaterial) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# The clip\n\nThis short is %s to %s of the long video (%.1f s).\n\n",
		clock(m.clip.Start()), clock(m.clip.End()), m.clip.Length().Seconds())
	b.WriteString("## The window\n\nThe long video's sentences around it, numbered, with times in the window. " +
		"The short speaks those marked ▶. To move the cut (when a reviewer asks to start or end elsewhere), run " +
		"`ekin tool clip --first N --last M`.\n\n")
	for i, x := range m.windowSentences {
		mark := " "
		if x.Start >= m.window.Start-0.02 && x.End <= m.window.End+0.02 {
			mark = "▶"
		}
		fmt.Fprintf(&b, "%s %d. %.2f–%.2f s: %s\n", mark, i+1, x.Start, x.End, x.Text)
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
