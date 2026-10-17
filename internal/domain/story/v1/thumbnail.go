package story

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/pkg/timeutil"
)

// Limits of a long video's thumbnail variants (docs/thumbnails.md).
const (
	MaxThumbnails       = 4
	MaxThumbnailWords   = 5
	maxThumbnailRunes   = 60
	maxThumbnailConcept = 40
)

// Thumbnail is one designed thumbnail of a long video: one concept (a
// strong still with a hook, bold type, a contrast) and the words on it.
// Still is the composition time of the frame it uses, if any.
type Thumbnail struct {
	N       int      `json:"n"`
	Concept string   `json:"concept"`
	Text    string   `json:"text"`
	Still   *float64 `json:"still,omitempty"`
}

// Thumbnails are a revision's thumbnail variants and the one the agent
// recommends. Zero when the revision has none.
type Thumbnails struct {
	Recommended int         `json:"recommended"`
	Variants    []Thumbnail `json:"variants"`
}

// ThumbnailArtifact is the revision artifact holding variant n.
func ThumbnailArtifact(n int) string { return fmt.Sprintf("thumb-%d.jpg", n) }

// IsZero reports whether there are no variants.
func (t Thumbnails) IsZero() bool { return len(t.Variants) == 0 }

// Has reports whether variant n exists.
func (t Thumbnails) Has(n int) bool {
	for _, v := range t.Variants {
		if v.N == n {
			return true
		}
	}
	return false
}

// ParseThumbnails reads thumbnails.json. want is how many variants the
// video asks for (1–4); 0 takes any count up to MaxThumbnails. Numbers
// are unique, the recommended one is among them, and every variant has a
// concept and at most MaxThumbnailWords words of text.
func ParseThumbnails(raw []byte, want int) (Thumbnails, error) {
	invalid := func(format string, a ...any) (Thumbnails, error) {
		return Thumbnails{}, apperror.New(apperror.ValidationFailed, "thumbnails.json: "+fmt.Sprintf(format, a...))
	}
	var t Thumbnails
	if err := json.Unmarshal(raw, &t); err != nil {
		return invalid("not valid JSON: %v", err)
	}
	switch n := len(t.Variants); {
	case want > 0 && n != want:
		return invalid("has %d variants; this video needs %d", n, want)
	case n == 0:
		return invalid("has no variants")
	case n > MaxThumbnails:
		return invalid("has %d variants; at most %d", n, MaxThumbnails)
	}
	seen := map[int]bool{}
	for i := range t.Variants {
		v := &t.Variants[i]
		if v.N < 1 || v.N > MaxThumbnails || seen[v.N] {
			return invalid("variant numbers must be unique, from 1 to %d", MaxThumbnails)
		}
		seen[v.N] = true
		var err error
		if *v, err = v.checked(); err != nil {
			return Thumbnails{}, err
		}
	}
	if !t.Has(t.Recommended) {
		return invalid("recommended is %d, which is not one of the variants", t.Recommended)
	}
	return t, nil
}

func (v Thumbnail) checked() (Thumbnail, error) {
	invalid := func(format string, a ...any) (Thumbnail, error) {
		return Thumbnail{}, apperror.New(apperror.ValidationFailed, fmt.Sprintf("thumbnail %d: ", v.N)+fmt.Sprintf(format, a...))
	}
	v.Concept = strings.TrimSpace(v.Concept)
	v.Text = strings.Join(strings.Fields(v.Text), " ")
	switch {
	case v.Concept == "":
		return invalid("concept is required")
	case utf8.RuneCountInString(v.Concept) > maxThumbnailConcept:
		return invalid("concept is over %d characters", maxThumbnailConcept)
	case v.Text == "":
		return invalid("text is required")
	case len(strings.Fields(v.Text)) > MaxThumbnailWords:
		return invalid("text has %d words; at most %d", len(strings.Fields(v.Text)), MaxThumbnailWords)
	case utf8.RuneCountInString(v.Text) > maxThumbnailRunes:
		return invalid("text is over %d characters", maxThumbnailRunes)
	case v.Still != nil && (*v.Still < 0 || math.IsNaN(*v.Still) || math.IsInf(*v.Still, 0)):
		return invalid("still must be a time in seconds")
	}
	return v, nil
}

// WithText is t with variant n's words replaced by text, checked as
// ParseThumbnails checks them.
func (t Thumbnails) WithText(n int, text string) (Thumbnails, error) {
	out := Thumbnails{Recommended: t.Recommended, Variants: make([]Thumbnail, len(t.Variants))}
	found := false
	for i, v := range t.Variants {
		if v.N == n {
			v.Text, found = text, true
			var err error
			if v, err = v.checked(); err != nil {
				return Thumbnails{}, err
			}
		}
		out.Variants[i] = v
	}
	if !found {
		return Thumbnails{}, apperror.New(apperror.NotFound, fmt.Sprintf("the revision has no thumbnail %d", n))
	}
	return out, nil
}

// Thumbnail is the variant a person picked for the video; 0 when nobody
// did, and the recommended one is used.
func (d *Deliverable) Thumbnail() int { return d.thumbnail }

// ThumbnailFor is the variant of t the video goes out with: the one
// picked, else the recommended one, else 0 (the poster frame).
func (d *Deliverable) ThumbnailFor(t Thumbnails) int {
	switch {
	case t.Has(d.thumbnail):
		return d.thumbnail
	case t.Has(t.Recommended):
		return t.Recommended
	}
	return 0
}

// PickThumbnail picks variant n of the current revision's thumbnails t
// for a video in review or approved but not yet published; 0 goes back
// to the recommended one.
func (s *Story) PickThumbnail(id uuid.UUID, n int, t Thumbnails, now time.Time) error {
	d, err := s.find(id, StatusReview, StatusFinalizing, StatusReady)
	if err != nil {
		return err
	}
	if !d.IsVideo() || t.IsZero() {
		return apperror.New(apperror.ValidationFailed, "this revision has no thumbnails to pick from")
	}
	if n != 0 && !t.Has(n) {
		return apperror.New(apperror.ValidationFailed, fmt.Sprintf("the revision has no thumbnail %d", n))
	}
	d.thumbnail = n
	d.updatedAt = timeutil.Normalize(now)
	s.touch(now)
	return nil
}

// EditedThumbnails records a person's edit of a long video's thumbnail
// words, made in place on the variants t, as the next revision. The video
// is unchanged; the deliverable goes to rendering so the thumbnails are
// drawn again, and keeps its pick.
func (s *Story) EditedThumbnails(id uuid.UUID, artifacts map[string]string, by uuid.UUID, t Thumbnails, now time.Time) (Revision, error) {
	d, err := s.find(id, StatusReview)
	if err != nil {
		return Revision{}, err
	}
	if !d.IsVideo() || t.IsZero() || s.atScript(d) {
		return Revision{}, apperror.New(apperror.ValidationFailed, "this video has no thumbnails to edit")
	}
	d.revision++
	rev := Revision{
		DeliverableID: d.id, Number: d.revision, Artifacts: maps.Clone(artifacts), EditedBy: by,
		Notes: "Thumbnail text edited in place.", Measures: d.measures, Thumbnails: t, CreatedAt: timeutil.Normalize(now),
	}
	d.set(StatusRendering, now)
	s.touch(now)
	return rev, nil
}

// IsThumbnailArtifact reports whether name is a thumbnail variant's file.
func IsThumbnailArtifact(name string) bool {
	for n := 1; n <= MaxThumbnails; n++ {
		if name == ThumbnailArtifact(n) {
			return true
		}
	}
	return false
}

// ThumbnailsOnly reports whether the revision changed only its
// thumbnails, so its render draws them alone and keeps the video of the
// revision before: the agent addressed thumbnail-only feedback, or a
// person edited a variant's words in place.
func (r Revision) ThumbnailsOnly() bool {
	return r.Feedback.ThumbnailOnly() || (r.EditedBy != uuid.Nil && !r.Thumbnails.IsZero())
}

// ThumbnailOnly reports whether every note is about the thumbnails: the
// agent then changes only them, and the video is not rendered again.
func (f *Feedback) ThumbnailOnly() bool {
	if f == nil || len(f.Notes) == 0 {
		return false
	}
	for _, n := range f.Notes {
		if n.Scope != ScopeThumbnail {
			return false
		}
	}
	return true
}
