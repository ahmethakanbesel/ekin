package speech

import (
	"slices"
	"testing"
)

func TestRespell(t *testing.T) {
	lex := map[string]string{"PHQ-9": "pe ha ku dokuz", "acme": "ek mi", " BDT ": " be de te "}
	tests := []struct {
		in, want string
	}{
		{"PHQ-9", "pe ha ku dokuz"},
		{"phq-9,", "pe ha ku dokuz,"},
		{"(PHQ-9)", "(pe ha ku dokuz)"},
		{"PHQ-9'da", "pe ha ku dokuz'da"},
		{"PHQ-9’un,", "pe ha ku dokuz’un,"},
		{"Acme'nın", "ek mi'nın"},
		{"BDT", "be de te"},
		{"PHQ-90", "PHQ-90"},
		{"'PHQ-9'", "'pe ha ku dokuz'"},
		{"PHQ'da", "PHQ'da"},
		{"kitap", "kitap"},
	}
	var in, want []string
	for _, tt := range tests {
		in, want = append(in, tt.in), append(want, tt.want)
	}
	if got := respell(in, lex); !slices.Equal(got, want) {
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("respell(%q) = %q, want %q", in[i], got[i], want[i])
			}
		}
	}
}
