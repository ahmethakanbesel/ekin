package library

import "kurgu/internal/apperror"

// Mode is where a draft's media of one kind comes from.
type Mode string

const (
	// ModeFresh composes music and searches Pexels for stock, as drafts
	// always have. The zero Mode is unset: it takes the next layer's
	// mode (the workspace default) and, set nowhere, behaves as fresh.
	ModeFresh Mode = "fresh"
	// ModeLibrary uses the library only: no composing, no searching.
	ModeLibrary Mode = "library"
	// ModeMixed looks in the library first and composes or searches only
	// when nothing there fits.
	ModeMixed Mode = "mixed"
)

// ParseMode parses a mode; "" is unset.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "":
		return "", nil
	case string(ModeFresh):
		return ModeFresh, nil
	case string(ModeLibrary):
		return ModeLibrary, nil
	case string(ModeMixed):
		return ModeMixed, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "media source must be fresh, library or mixed")
}

// String is the mode's wire name; unset reads as fresh, which is how
// it behaves.
func (m Mode) String() string {
	if m == "" {
		return string(ModeFresh)
	}
	return string(m)
}

func (m Mode) over(base Mode) Mode {
	if m == "" {
		return base
	}
	return m
}

// Policy picks a Mode per kind for a pipeline's or a request's drafts.
// An unset kind takes the workspace default; set nowhere, it is fresh.
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

// Over fills p's unset kinds from base.
func (p Policy) Over(base Policy) Policy {
	return Policy{Music: p.Music.over(base.Music), Video: p.Video.over(base.Video), Photo: p.Photo.over(base.Photo)}
}

// UsesLibrary reports whether drafts may take kind k from the library.
func (p Policy) UsesLibrary(k Kind) bool {
	m := p.Mode(k)
	return m == ModeLibrary || m == ModeMixed
}

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
