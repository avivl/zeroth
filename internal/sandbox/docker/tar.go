package docker

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/avivl/zeroth/internal/sandbox"
)

// ExportTar implements [sandbox.Driver].
func (d *Driver) ExportTar(ctx context.Context, id sandbox.ID, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker export: %w", err)
	}
	inst, err := d.lookup(id)
	if err != nil {
		return fmt.Errorf("sandbox docker export: %w", err)
	}
	inst.mu.Lock()
	stopped := inst.stopped
	dir := inst.workspace
	inst.mu.Unlock()
	if stopped {
		return fmt.Errorf("sandbox docker export: %w", sandbox.ErrStopped)
	}
	// Host-side tar of the workspace. Exec is docker exec, so this
	// does not take a turn lock.
	if err := packTar(ctx, dir, w); err != nil {
		return fmt.Errorf("sandbox docker export: %w", err)
	}
	return nil
}

// ImportTar implements [sandbox.Driver].
func (d *Driver) ImportTar(ctx context.Context, id sandbox.ID, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	inst, err := d.lookup(id)
	if err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	inst.mu.Lock()
	stopped := inst.stopped
	dir := inst.workspace
	inst.mu.Unlock()
	if stopped {
		return fmt.Errorf("sandbox docker import: %w", sandbox.ErrStopped)
	}
	incoming, err := os.MkdirTemp(filepath.Dir(dir), "zeroth-import-")
	if err != nil {
		return fmt.Errorf("sandbox docker import: temp: %w", err)
	}
	defer func() { _ = os.RemoveAll(incoming) }()
	if err := unpackTar(incoming, r); err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	if err := clearDir(dir); err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	entries, err := os.ReadDir(incoming)
	if err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	for _, e := range entries {
		from := filepath.Join(incoming, e.Name())
		to := filepath.Join(dir, e.Name())
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("sandbox docker import: %w", err)
		}
	}
	return nil
}

func packTar(ctx context.Context, dir string, w io.Writer) error {
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

func unpackTar(dir string, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
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
		case tar.TypeReg, tar.TypeRegA:
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
		return "", fmt.Errorf("%w: tar path escapes dest: %q", sandbox.ErrInvalid, name)
	}
	return full, nil
}

func clearDir(dir string) error {
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
