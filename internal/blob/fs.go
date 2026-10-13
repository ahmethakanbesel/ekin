package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// FS stores blobs as files under a root directory.
type FS struct {
	root string
}

var _ Store = (*FS)(nil)

// NewFS returns a store rooted at dir, creating it.
func NewFS(dir string) (*FS, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("blob: create root: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &FS{root: abs}, nil
}

func (s *FS) path(key string) (string, error) {
	if err := CheckKey(key); err != nil {
		return "", err
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

// Put writes to a temporary file and renames it into place, so readers
// never see a partial object.
func (s *FS) Put(ctx context.Context, key string, r io.Reader, _ int64) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("blob: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return fmt.Errorf("blob: temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, contextReader{ctx, r}); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("blob: write %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("blob: close %s: %w", key, err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("blob: rename %s: %w", key, err)
	}
	return nil
}

func (s *FS) Open(_ context.Context, key string) (io.ReadCloser, Info, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, Info{}, err
	}
	f, err := os.Open(p) //nolint:gosec // path is checked against the root
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Info{}, ErrNotFound
	}
	if err != nil {
		return nil, Info{}, fmt.Errorf("blob: open %s: %w", key, err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, Info{}, err
	}
	return f, Info{Size: st.Size(), ModTime: st.ModTime()}, nil
}

func (s *FS) Stat(_ context.Context, key string) (Info, error) {
	p, err := s.path(key)
	if err != nil {
		return Info{}, err
	}
	st, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, ErrNotFound
	}
	if err != nil {
		return Info{}, err
	}
	return Info{Size: st.Size(), ModTime: st.ModTime()}, nil
}

func (s *FS) Delete(_ context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("blob: delete %s: %w", key, err)
	}
	return nil
}

func (s *FS) DeletePrefix(_ context.Context, prefix string) error {
	p, err := s.path(prefix)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("blob: delete %s: %w", prefix, err)
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
