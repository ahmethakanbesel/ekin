package library_test

import (
	"reflect"
	"testing"

	"kurgu/internal/apperror"
	library "kurgu/internal/domain/library/v1"
)

func TestParseMode(t *testing.T) {
	tests := map[string]library.Mode{"": "", "fresh": library.ModeFresh, "library": library.ModeLibrary, "mixed": library.ModeMixed}
	for in, want := range tests {
		if got, err := library.ParseMode(in); err != nil || got != want {
			t.Errorf("%q = %q, %v", in, got, err)
		}
	}
	if _, err := library.ParseMode("only"); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("bad mode err = %v", err)
	}
	if library.ModeFresh.String() != "fresh" || library.Mode("").String() != "fresh" || library.ModeMixed.String() != "mixed" {
		t.Fatal("wire names")
	}
}

func TestPolicy(t *testing.T) {
	p := library.Policy{Music: library.ModeLibrary, Video: library.ModeMixed}
	tests := []struct {
		kind           library.Kind
		library, fresh bool
	}{
		{library.KindMusic, true, false},
		{library.KindVideo, true, true},
		{library.KindPhoto, false, true},
	}
	for _, tt := range tests {
		if p.UsesLibrary(tt.kind) != tt.library || p.AllowsFresh(tt.kind) != tt.fresh {
			t.Errorf("%s: library %v fresh %v", tt.kind, p.UsesLibrary(tt.kind), p.AllowsFresh(tt.kind))
		}
	}
	if got := p.LibraryKinds(); !reflect.DeepEqual(got, []library.Kind{library.KindMusic, library.KindVideo}) {
		t.Fatalf("library kinds = %v", got)
	}
	if (library.Policy{}).LibraryKinds() != nil || (library.Policy{}).Check() != nil {
		t.Fatal("zero policy is not all fresh")
	}
	explicit := library.Policy{Music: library.ModeFresh, Video: library.ModeFresh, Photo: library.ModeFresh}
	if explicit.LibraryKinds() != nil || !explicit.AllowsFresh(library.KindMusic) || explicit.Check() != nil {
		t.Fatal("explicit fresh does not behave as unset")
	}
	if err := (library.Policy{Photo: "everything"}).Check(); !apperror.Is(err, apperror.ValidationFailed) {
		t.Fatalf("bad policy err = %v", err)
	}
}

func TestPolicyOver(t *testing.T) {
	base := library.Policy{Music: library.ModeLibrary, Video: library.ModeMixed, Photo: library.ModeFresh}
	tests := []struct {
		name    string
		p, base library.Policy
		want    library.Policy
	}{
		{"unset takes base", library.Policy{}, base, base},
		{"set wins", library.Policy{Music: library.ModeFresh, Photo: library.ModeMixed}, base,
			library.Policy{Music: library.ModeFresh, Video: library.ModeMixed, Photo: library.ModeMixed}},
		{"over nothing", library.Policy{Video: library.ModeLibrary}, library.Policy{}, library.Policy{Video: library.ModeLibrary}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Over(tt.base); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
