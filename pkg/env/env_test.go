package env_test

import (
	"os"
	"testing"

	"ekin/pkg/env"
)

func TestLookup(t *testing.T) {
	tests := []struct {
		name    string
		set     map[string]string
		key     string
		want    string
		wantSet bool
	}{
		{"new name", map[string]string{"EKIN_X": "new"}, "EKIN_X", "new", true},
		{"legacy name", map[string]string{"KURGU_X": "old"}, "EKIN_X", "old", true},
		{"new wins", map[string]string{"EKIN_X": "new", "KURGU_X": "old"}, "EKIN_X", "new", true},
		{"empty new falls back", map[string]string{"EKIN_X": "", "KURGU_X": "old"}, "EKIN_X", "old", true},
		{"empty new, no legacy", map[string]string{"EKIN_X": ""}, "EKIN_X", "", true},
		{"unset", nil, "EKIN_X", "", false},
		{"other prefix", map[string]string{"KURGU_PATH_X": "old"}, "PATH_X", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"EKIN_X", "KURGU_X", "PATH_X", "KURGU_PATH_X"} {
				t.Setenv(k, "") // restores the variable after the test
				_ = os.Unsetenv(k)
			}
			for k, v := range tt.set {
				t.Setenv(k, v)
			}
			got, ok := env.Lookup(tt.key)
			if got != tt.want || ok != tt.wantSet {
				t.Fatalf("Lookup(%q) = %q, %v; want %q, %v", tt.key, got, ok, tt.want, tt.wantSet)
			}
			if g := env.Get(tt.key); g != tt.want {
				t.Fatalf("Get(%q) = %q, want %q", tt.key, g, tt.want)
			}
		})
	}
}
