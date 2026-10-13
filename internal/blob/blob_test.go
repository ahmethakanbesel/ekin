package blob_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"kurgu/internal/blob"
)

func TestFS(t *testing.T) {
	ctx := context.Background()
	s, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "ws/a/doc.txt", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	r, info, err := s.Open(ctx, "ws/a/doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "hello" || info.Size != 5 {
		t.Fatalf("read %q size %d", b, info.Size)
	}
	_ = s.Put(ctx, "ws/a/other.txt", strings.NewReader("x"), 1)
	if err := s.DeletePrefix(ctx, "ws/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat(ctx, "ws/a/other.txt"); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("stat after delete = %v", err)
	}
	if err := s.Delete(ctx, "ws/a/doc.txt"); err != nil {
		t.Fatalf("delete missing = %v", err)
	}
	for _, bad := range []string{"", "/etc/passwd", "../x", "a/../../x", "a//b", "a\\b"} {
		if err := s.Put(ctx, bad, strings.NewReader(""), 0); err == nil {
			t.Errorf("key %q accepted", bad)
		}
	}
}
