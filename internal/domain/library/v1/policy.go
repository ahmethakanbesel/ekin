package library

import "kurgu/internal/apperror"

// Mode is where a draft's media of one kind comes from.
type Mode string

const (
	// ModeFresh composes music and searches Pexels for stock, as drafts
	// always have (the default).
	ModeFresh Mode = ""
	// ModeLibrary uses the library only: no composing, no searching.
	ModeLibrary Mode = "library"
	// ModeMixed looks in the library first and composes or searches only
	// when nothing there fits.
	ModeMixed Mode = "mixed"
)

// ParseMode parses a mode; "" and "fresh" are the default.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "", "fresh":
		return ModeFresh, nil
	case string(ModeLibrary):
		return ModeLibrary, nil
	case string(ModeMixed):
		return ModeMixed, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "media source must be fresh, library or mixed")
}

// String is the mode's wire name.
func (m Mode) String() string {
	if m == ModeFresh {
		return "fresh"
	}
	return string(m)
}

// Policy picks a Mode per kind for a pipeline's or a request's drafts.
// The zero value makes everything fresh.
type Policy struct {
	Music Mode `json:"music,omitempty"`
	Video Mode `json:"video,omitempty"`
	Photo Mode `json:"photo,omitempty"`
}

// Check reports whether every mode is valid.
func (p Policy) Check() error {
	for _, m := range []Mode{p.Music, p.Video, p.Photo} {
		if _, err := ParseMode(string(m)); err != nil {
			return err
		}
	}
	return nil
}

// Mode is the mode of kind k.
func (p Policy) Mode(k Kind) Mode {
	switch k {
	case KindMusic:
		return p.Music
	case KindVideo:
		return p.Video
	case KindPhoto:
		return p.Photo
	}
	return ModeFresh
}

// UsesLibrary reports whether drafts may take kind k from the library.
func (p Policy) UsesLibrary(k Kind) bool { return p.Mode(k) != ModeFresh }

// AllowsFresh reports whether drafts may compose or search for kind k.
func (p Policy) AllowsFresh(k Kind) bool { return p.Mode(k) != ModeLibrary }

// LibraryKinds lists the kinds drafts may take from the library.
func (p Policy) LibraryKinds() []Kind {
	var out []Kind
	for _, k := range Kinds {
		if p.UsesLibrary(k) {
			out = append(out, k)
		}
	}
	return out
}
