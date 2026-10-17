// Package env reads EKIN_* environment variables, falling back to the
// KURGU_* name the product used before it was renamed, so existing
// .env files and hosts keep working.
package env

import (
	"os"
	"strings"
)

const (
	prefix = "EKIN_"
	legacy = "KURGU_"
)

// Get returns the variable named key, or its KURGU_ twin when key is
// unset or empty.
func Get(key string) string {
	v, _ := Lookup(key)
	return v
}

// Lookup is os.LookupEnv with the KURGU_ fallback: the EKIN_ name wins
// when it is set and not empty.
func Lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if ok && v != "" {
		return v, true
	}
	if rest, isOurs := strings.CutPrefix(key, prefix); isOurs {
		if old, found := os.LookupEnv(legacy + rest); found {
			return old, true
		}
	}
	return v, ok
}
