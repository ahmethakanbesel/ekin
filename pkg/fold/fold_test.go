package fold_test

import (
	"testing"

	"ekin/pkg/fold"
)

func TestString(t *testing.T) {
	tests := map[string]string{
		"Kaygı İLE Endişe: Fark?": "kaygi ile endise fark",
		"ISPARTA":                 "isparta",
		"  Çocuk -- uykusu ":      "cocuk uykusu",
		"%_":                      "",
		"PHQ-9 testi":             "phq 9 testi",
	}
	for in, want := range tests {
		if got := fold.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
}
