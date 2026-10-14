// Package archive packs a working directory as a gzipped tar and unpacks
// it again, refusing entries that would escape the target directory.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// skipInArchive are work-dir entries not worth keeping: frame previews
// and source downloads that were transcoded.
func skipInArchive(rel string, d fs.DirEntry) bool {
	if d.IsDir() {
		return rel == "frames" || rel == ".claude"
	}
	return strings.HasSuffix(rel, ".src.mp4")
}

// TarGz writes dir as a gzipped tar to w.
func TarGz(dir string, w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if skipInArchive(rel, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil // no symlinks or devices
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = rel
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(p) //nolint:gosec // walking our own work dir
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		_ = f.Close()
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Untar extracts a gzipped tar into dir, refusing paths that escape it.
func Untar(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(h.Name))
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive entry %q escapes the work dir", h.Name)
		}
		target := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // checked above
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, 2<<30)); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

// ReadFiles returns the named files (slash paths) from a gzipped tar,
// each read up to limit bytes; missing names are absent from the map.
func ReadFiles(r io.Reader, limit int64, names ...string) (map[string][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	out := map[string][]byte{}
	tr := tar.NewReader(gz)
	for len(out) < len(want) {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg || !want[h.Name] {
			continue
		}
		if h.Size > limit {
			return nil, fmt.Errorf("%s is larger than %d bytes", h.Name, limit)
		}
		b, err := io.ReadAll(io.LimitReader(tr, limit))
		if err != nil {
			return nil, err
		}
		out[h.Name] = b
	}
	return out, nil
}
