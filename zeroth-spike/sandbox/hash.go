package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// HashTree returns a content hash of a workspace directory. Paths,
// file types, permission bits, and file bytes (or symlink targets)
// are included. Mtimes and ownership are not: tar round-trips change
// those even when the tree is byte-identical.
func HashTree(root string) ([32]byte, error) {
	var out [32]byte
	var rels []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rels = append(rels, rel)
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("hash tree walk: %w", err)
	}
	sort.Strings(rels)

	h := sha256.New()
	for _, rel := range rels {
		path := filepath.Join(root, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return out, fmt.Errorf("hash tree lstat %s: %w", rel, err)
		}
		kind := 'f'
		switch {
		case info.IsDir():
			kind = 'd'
		case info.Mode()&os.ModeSymlink != 0:
			kind = 'l'
		case !info.Mode().IsRegular():
			kind = 's'
		}
		if _, err := fmt.Fprintf(h, "%s\n%c\n%o\n", filepath.ToSlash(rel), kind, info.Mode().Perm()); err != nil {
			return out, err
		}
		switch kind {
		case 'f':
			if _, err := fmt.Fprintf(h, "%d\n", info.Size()); err != nil {
				return out, err
			}
			f, err := os.Open(path)
			if err != nil {
				return out, fmt.Errorf("hash tree open %s: %w", rel, err)
			}
			_, copyErr := io.Copy(h, f)
			_ = f.Close()
			if copyErr != nil {
				return out, fmt.Errorf("hash tree read %s: %w", rel, copyErr)
			}
		case 'l':
			target, err := os.Readlink(path)
			if err != nil {
				return out, fmt.Errorf("hash tree readlink %s: %w", rel, err)
			}
			if _, err := io.WriteString(h, target); err != nil {
				return out, err
			}
		}
	}
	copy(out[:], h.Sum(nil))
	return out, nil
}

// HashHex encodes a tree hash as lowercase hex.
func HashHex(sum [32]byte) string {
	return hex.EncodeToString(sum[:])
}

// HashWorkspace hashes the live workspace of an instance.
func HashWorkspace(inst Instance) ([32]byte, error) {
	dir, err := workspaceDir(inst)
	if err != nil {
		return [32]byte{}, err
	}
	sum, err := HashTree(dir)
	if err != nil {
		return [32]byte{}, fmt.Errorf("hash workspace: %w", err)
	}
	return sum, nil
}

func workspaceDir(inst Instance) (string, error) {
	switch x := inst.(type) {
	case *dockerInstance:
		return x.merged, nil
	case *memoryInstance:
		return x.dir, nil
	default:
		return "", fmt.Errorf("hash workspace: unsupported instance %T", inst)
	}
}

// OverlayMethod reports how the workspace is mounted. Tests and the
// gate use this to record overlayfs vs fuse-overlayfs vs a plain dir.
func OverlayMethod(inst Instance) string {
	switch x := inst.(type) {
	case *dockerInstance:
		return x.overlay
	case *memoryInstance:
		return "none"
	default:
		return "unknown"
	}
}
