// Package timeutil holds the one time representation rule: instants are
// UTC at millisecond precision, stored as Unix milliseconds.
package timeutil

import "time"

// Normalize returns t in UTC truncated to milliseconds, the precision
// instants are stored at, so a value survives a storage round trip
// unchanged.
func Normalize(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Truncate(time.Millisecond)
}

// ToMillis returns t as Unix milliseconds; the zero time maps to 0.
func ToMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// FromMillis is the inverse of ToMillis; 0 maps to the zero time.
func FromMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
