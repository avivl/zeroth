package sandbox

import (
	"path"
	"strings"
)

// ValidCredPath reports whether p is an allowed Exec file-credential
// destination. Credentials go on a tmpfs (CredsDir or /tmp), never
// into the workspace (Z1-113).
func ValidCredPath(p string) error {
	if p == "" || strings.ContainsRune(p, 0) {
		return ErrInvalid
	}
	if !strings.HasPrefix(p, "/") {
		return ErrInvalid
	}
	clean := path.Clean("/" + strings.TrimPrefix(filepathToSlash(p), "/"))
	if clean == "/" || clean == "." {
		return ErrInvalid
	}
	if underDir(clean, WorkspaceDir) {
		return ErrInvalid
	}
	if underDir(clean, CredsDir) || underDir(clean, "/tmp") {
		return nil
	}
	return ErrInvalid
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

func underDir(clean, dir string) bool {
	if clean == dir {
		// The mount point itself is not a file destination.
		return false
	}
	return strings.HasPrefix(clean, dir+"/")
}
