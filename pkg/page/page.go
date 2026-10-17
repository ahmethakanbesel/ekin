// Package page holds keyset pagination: a listing returns at most a
// limit of rows and an opaque cursor naming the last one, and the next
// call continues after it. Unlike an offset, a cursor neither skips nor
// repeats rows when rows are added meanwhile.
package page

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// ErrInvalid is a cursor this package did not make.
var ErrInvalid = errors.New("invalid cursor")

// Encode returns the cursor for key, the sort key of a listing's last
// row (a struct of the ordered columns).
func Encode(key any) string {
	raw, err := json.Marshal(key)
	if err != nil {
		panic("page: cursor key does not encode: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Decode reads cursor into key. An empty cursor leaves key alone and
// reports false: the listing starts at its beginning.
func Decode(cursor string, key any) (bool, error) {
	if cursor == "" {
		return false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || json.Unmarshal(raw, key) != nil {
		return false, ErrInvalid
	}
	return true, nil
}

// Limit is n within 1..maxN, or def when n is not positive.
func Limit(n, def, maxN int) int {
	switch {
	case n <= 0:
		return def
	case n > maxN:
		return maxN
	}
	return n
}
