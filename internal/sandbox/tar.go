package sandbox

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PackOverlay writes an uncompressed tar of dir. Paths matching
// ExcludedFromExport are omitted. Byte-identity of a checkpoint is the
// content hash of the remaining tree, not this tar file: GNU headers
// carry mtime and owners that change across a round-trip.
func PackOverlay(ctx context.Context, dir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	defer tw.Close()
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if ExcludedFromExport(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hdr.Linkname = target
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		_ = f.Close()
		return copyErr
	})
}

// UnpackOverlay writes r's tar into dir. Paths that escape dir are
// ErrInvalid. Paths matching ExcludedFromImport are skipped.
func UnpackOverlay(dir string, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if ExcludedFromImport(hdr.Name) {
			continue
		}
		target, err := safeJoin(dir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
		case tar.TypeReg, 0:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent: %w", err)
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			if _, err := io.Copy(w, tr); err != nil {
				_ = w.Close()
				return fmt.Errorf("write file: %w", err)
			}
			if err := w.Close(); err != nil {
				return fmt.Errorf("close file: %w", err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent: %w", err)
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("symlink: %w", err)
			}
		default:
			// Skip specials. Workspace checkpoints are files, dirs, links.
		}
	}
}

// ReplaceOverlay replaces dir's contents with r's tar. Unpack happens
// into a sibling temp directory first so a rejected tar does not leave
// dir empty.
func ReplaceOverlay(dir string, r io.Reader) error {
	incoming, err := os.MkdirTemp(filepath.Dir(dir), "zeroth-import-")
	if err != nil {
		return fmt.Errorf("temp: %w", err)
	}
	defer func() { _ = os.RemoveAll(incoming) }()
	if err := UnpackOverlay(incoming, r); err != nil {
		return err
	}
	if err := ClearOverlay(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(incoming)
	if err != nil {
		return fmt.Errorf("read import: %w", err)
	}
	for _, e := range entries {
		from := filepath.Join(incoming, e.Name())
		to := filepath.Join(dir, e.Name())
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("install %s: %w", e.Name(), err)
		}
	}
	return nil
}

// ClearOverlay deletes dir's children. dir itself stays.
func ClearOverlay(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("clear dir: %w", err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("clear dir %s: %w", e.Name(), err)
		}
	}
	return nil
}

func safeJoin(dir, name string) (string, error) {
	dest, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return "", err
	}
	name = strings.ReplaceAll(name, `\`, "/")
	for strings.HasPrefix(name, "/") {
		name = strings.TrimPrefix(name, "/")
	}
	if name == "" || name == "." {
		return dest, nil
	}
	full := filepath.Clean(filepath.Join(dest, filepath.FromSlash(name)))
	rel, err := filepath.Rel(dest, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: tar path escapes dest: %q", ErrInvalid, name)
	}
	return full, nil
}
