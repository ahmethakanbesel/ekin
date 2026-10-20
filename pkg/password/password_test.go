package password_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ahmethakanbesel/ekin/pkg/password"
)

var cheap = password.Params{Memory: 64, Time: 1, Threads: 1}

func TestHashVerify(t *testing.T) {
	h := password.New(cheap)
	encoded, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("encoded = %s", encoded)
	}
	again, _ := h.Hash("correct horse battery")
	if again == encoded {
		t.Fatal("hash is not salted")
	}
	tests := []struct {
		plain string
		want  bool
	}{
		{"correct horse battery", true},
		{"correct horse batter", false},
		{"", false},
	}
	for _, tt := range tests {
		ok, err := h.Verify(encoded, tt.plain)
		if err != nil || ok != tt.want {
			t.Errorf("Verify(%q) = %v, %v; want %v", tt.plain, ok, err, tt.want)
		}
	}
}

func TestVerifyUsesStoredParams(t *testing.T) {
	encoded, _ := password.New(cheap).Hash("pw-0123456789")
	ok, err := password.New(password.Default).Verify(encoded, "pw-0123456789")
	if err != nil || !ok {
		t.Fatalf("verify with other params = %v, %v", ok, err)
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	h := password.New(cheap)
	for _, encoded := range []string{
		"",
		"plain",
		"$argon2i$v=19$m=64,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=18$m=64,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=0,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=99999999,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=64,t=1,p=1$!!$a2V5",
		"$argon2id$v=19$m=64,t=1,p=1$c2FsdA$",
	} {
		if _, err := h.Verify(encoded, "x"); !errors.Is(err, password.ErrMalformed) {
			t.Errorf("Verify(%q) err = %v, want ErrMalformed", encoded, err)
		}
	}
}
