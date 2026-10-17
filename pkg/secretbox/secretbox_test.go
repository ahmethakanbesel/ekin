package secretbox_test

import (
	"path/filepath"
	"testing"

	"ekin/pkg/secretbox"
)

func TestSealSign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	b, err := secretbox.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed := b.Seal("refresh-token")
	if sealed == "refresh-token" {
		t.Fatal("not encrypted")
	}
	again, _ := secretbox.LoadOrCreate(path)
	if plain, err := again.Open(sealed); err != nil || plain != "refresh-token" {
		t.Fatalf("open = %q, %v", plain, err)
	}
	other, _ := secretbox.New([]byte("a-completely-different-secret"))
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("opened with the wrong key")
	}
	signed := b.Sign("ws:123")
	if v, err := again.Verify(signed); err != nil || v != "ws:123" {
		t.Fatalf("verify = %q, %v", v, err)
	}
	if _, err := again.Verify("ws:124" + signed[6:]); err == nil {
		t.Fatal("tampered value verified")
	}
}
