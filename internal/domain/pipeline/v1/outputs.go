package pipeline

import (
	"fmt"
	"slices"
	"time"

	"ekin/internal/apperror"
)

// Language is the language a pipeline writes in.
type Language string

const (
	Turkish Language = "tr"
	English Language = "en"
)

// ParseLanguage parses a language code.
func ParseLanguage(s string) (Language, error) {
	switch l := Language(s); l {
	case Turkish, English:
		return l, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "language must be tr or en")
}

// Kind is a deliverable type a pipeline can produce.
type Kind string

const (
	KindVideo   Kind = "video"
	KindArticle Kind = "article"
)

// ParseKind parses a deliverable kind.
func ParseKind(s string) (Kind, error) {
	switch k := Kind(s); k {
	case KindVideo, KindArticle:
		return k, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "kind must be video or article")
}

// Format is a video's length class.
type Format string

const (
	FormatShort Format = "short"
	FormatLong  Format = "long"
)

// Orientation is a video's aspect.
type Orientation string

const (
	Vertical   Orientation = "vertical"   // 9:16
	Horizontal Orientation = "horizontal" // 16:9
	Square     Orientation = "square"     // 1:1
)

// Duration bounds per format. A YouTube Short is at most three minutes.
const (
	MinShortDuration = 10 * time.Second
	MaxShortDuration = 3 * time.Minute
	MinLongDuration  = time.Minute
	MaxLongDuration  = time.Hour
)

// MaxShortsPerVideo bounds how many shorts a long video may ask to be
// cut into.
const MaxShortsPerVideo = 20

// VideoSpec describes the video a pipeline produces.
type VideoSpec struct {
	format      Format
	orientation Orientation
	duration    time.Duration
	// shorts is how many shorts to cut from a long video once it is
	// live; 0 for none.
	shorts int
}

// NewVideoSpec returns a VideoSpec. Shorts must be vertical or square
// and between MinShortDuration and MaxShortDuration; long videos run
// MinLongDuration to MaxLongDuration in any orientation.
func NewVideoSpec(format Format, orientation Orientation, duration time.Duration) (VideoSpec, error) {
	switch orientation {
	case Vertical, Horizontal, Square:
	default:
		return VideoSpec{}, apperror.New(apperror.ValidationFailed, "orientation must be vertical, horizontal or square")
	}
	switch format {
	case FormatShort:
		if orientation == Horizontal {
			return VideoSpec{}, apperror.New(apperror.ValidationFailed, "a short must be vertical or square")
		}
		if duration < MinShortDuration || duration > MaxShortDuration {
			return VideoSpec{}, apperror.New(apperror.ValidationFailed, "a short must last between 10 seconds and 3 minutes")
		}
	case FormatLong:
		if duration < MinLongDuration || duration > MaxLongDuration {
			return VideoSpec{}, apperror.New(apperror.ValidationFailed, "a long video must last between 1 and 60 minutes")
		}
	default:
		return VideoSpec{}, apperror.New(apperror.ValidationFailed, "format must be short or long")
	}
	return VideoSpec{format: format, orientation: orientation, duration: duration.Truncate(time.Second)}, nil
}

// WithShorts returns v asking for n shorts to be cut from it once it is
// live (docs/shorts.md); only a long video can.
func (v VideoSpec) WithShorts(n int) (VideoSpec, error) {
	switch {
	case n < 0 || n > MaxShortsPerVideo:
		return VideoSpec{}, apperror.New(apperror.ValidationFailed, fmt.Sprintf("shorts per video must be between 0 and %d", MaxShortsPerVideo))
	case n > 0 && v.format != FormatLong:
		return VideoSpec{}, apperror.New(apperror.ValidationFailed, "only a long video can be cut into shorts")
	}
	v.shorts = n
	return v, nil
}

// Shorts is how many shorts to cut from the video once it is live.
func (v VideoSpec) Shorts() int { return v.shorts }

func (v VideoSpec) Format() Format           { return v.format }
func (v VideoSpec) Orientation() Orientation { return v.orientation }
func (v VideoSpec) Duration() time.Duration  { return v.duration }

// Article length bounds, in words.
const (
	MinArticleWords = 300
	MaxArticleWords = 6000
)

// ArticleSpec describes the article a pipeline produces.
type ArticleSpec struct {
	words        int
	translations []Language
}

// NewArticleSpec returns an ArticleSpec of about words words, translated
// into each of translations after approval. Duplicate translations are
// rejected rather than silently merged.
func NewArticleSpec(words int, translations []Language) (ArticleSpec, error) {
	if words < MinArticleWords || words > MaxArticleWords {
		return ArticleSpec{}, apperror.New(apperror.ValidationFailed, "article length must be between 300 and 6000 words")
	}
	seen := make(map[Language]bool, len(translations))
	for _, l := range translations {
		if _, err := ParseLanguage(string(l)); err != nil {
			return ArticleSpec{}, err
		}
		if seen[l] {
			return ArticleSpec{}, apperror.New(apperror.ValidationFailed, "a translation language is listed twice")
		}
		seen[l] = true
	}
	return ArticleSpec{words: words, translations: slices.Clone(translations)}, nil
}

func (a ArticleSpec) Words() int                   { return a.words }
func (a ArticleSpec) Translations() []Language     { return slices.Clone(a.translations) }
func (a ArticleSpec) TranslatesTo(l Language) bool { return slices.Contains(a.translations, l) }

// Outputs is what one pipeline run produces: a video, an article, or
// both. With both, the lead deliverable is produced first and the other
// is derived from it (a video's article expands and embeds it; an
// article's video is cut from it).
type Outputs struct {
	video   *VideoSpec
	article *ArticleSpec
	lead    Kind
}

// NewOutputs returns Outputs. At least one of video and article is
// required. lead may be empty when only one is given — it is that one —
// and must name a produced kind when both are given.
func NewOutputs(video *VideoSpec, article *ArticleSpec, lead Kind) (Outputs, error) {
	o := Outputs{video: video, article: article}
	switch {
	case video == nil && article == nil:
		return Outputs{}, apperror.New(apperror.ValidationFailed, "a pipeline must produce a video, an article, or both")
	case video != nil && article != nil:
		if lead != KindVideo && lead != KindArticle {
			return Outputs{}, apperror.New(apperror.ValidationFailed, "a pipeline producing both must say which one leads")
		}
		o.lead = lead
	case video != nil:
		if lead != "" && lead != KindVideo {
			return Outputs{}, apperror.New(apperror.ValidationFailed, "lead must be a produced kind")
		}
		o.lead = KindVideo
	default:
		if lead != "" && lead != KindArticle {
			return Outputs{}, apperror.New(apperror.ValidationFailed, "lead must be a produced kind")
		}
		o.lead = KindArticle
	}
	return o, nil
}

// Video returns the video spec, if the pipeline produces one.
func (o Outputs) Video() (VideoSpec, bool) {
	if o.video == nil {
		return VideoSpec{}, false
	}
	return *o.video, true
}

// Article returns the article spec, if the pipeline produces one.
func (o Outputs) Article() (ArticleSpec, bool) {
	if o.article == nil {
		return ArticleSpec{}, false
	}
	return *o.article, true
}

// Lead is the deliverable produced first.
func (o Outputs) Lead() Kind { return o.lead }

// Derived returns the deliverable derived from the lead, when the
// pipeline produces both.
func (o Outputs) Derived() (Kind, bool) {
	if o.video == nil || o.article == nil {
		return "", false
	}
	if o.lead == KindVideo {
		return KindArticle, true
	}
	return KindVideo, true
}

// Produces reports whether k is among the outputs.
func (o Outputs) Produces(k Kind) bool {
	switch k {
	case KindVideo:
		return o.video != nil
	case KindArticle:
		return o.article != nil
	}
	return false
}

func (o Outputs) isZero() bool { return o.lead == "" }
