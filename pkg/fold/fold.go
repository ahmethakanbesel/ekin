// Package fold normalises text for comparison: case, accents, the
// Turkish dotted and dotless i and punctuation do not matter. Stories
// fold titles to find repeats; listings fold titles to search them.
package fold

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// String folds s: lower-cased the Turkish way (I → ı, İ → i), accents
// dropped (ç → c, ş → s), ı as i, and every run of anything but letters
// and digits as one space, trimmed.
func String(s string) string {
	s = strings.NewReplacer("İ", "i", "I", "ı").Replace(s)
	s = strings.ToLower(s)
	s = norm.NFD.String(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
			// drop combining marks: â → a, ç → c, ş → s
		case r == 'ı':
			b.WriteRune('i')
			space = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		default:
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}
