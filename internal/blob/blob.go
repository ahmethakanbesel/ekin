// Package blob stores large objects (uploads, audio, renders) outside
// the database: on the local filesystem on a laptop, in S3-compatible
// storage on a server. Keys are slash-separated and never start with a
// slash, e.g. "ws/<id>/documents/<id>/original.pdf".
package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// ErrNotFound is returned for a key that does not exist.
var ErrNotFound = errors.New("blob: not found")

// Info describes a stored object.
type Info struct {
	Size    int64
	ModTime time.Time
}

// Store is the blob storage port.
type Store interface {
	// Put stores r under key, replacing any existing object. size may be
	// -1 when unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	// Open returns the object's content. Callers close it.
	Open(ctx context.Context, key string) (io.ReadCloser, Info, error)
	Stat(ctx context.Context, key string) (Info, error)
	// Delete removes key; deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes every key under prefix (a "directory").
	DeletePrefix(ctx context.Context, prefix string) error
}

// CheckKey rejects keys that could escape the store's root.
func CheckKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") || path.Clean(key) != key || key == "." ||
		strings.HasPrefix(key, "../") || key == ".." {
		return fmt.Errorf("blob: invalid key %q", key)
	}
	return nil
}

// Join builds a key from parts.
func Join(parts ...string) string { return path.Join(parts...) }
