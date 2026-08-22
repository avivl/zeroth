package server

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/store"
	"go.uber.org/zap"
)

func (s *Server) overlaySource(sess store.Session) string {
	repo := strings.TrimSpace(sess.Workspace.Repo)
	if repo != "" && isLocalDir(repo) {
		return repo
	}
	return strings.TrimSpace(s.workspaceRoot)
}

func isLocalDir(path string) bool {
	if strings.Contains(path, "://") || strings.HasPrefix(path, "git@") {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (s *Server) seedOverlay(sbx sandbox.ID, sess store.Session) error {
	src := s.overlaySource(sess)
	if src == "" {
		return nil
	}
	hw, ok := s.sandbox.(hostOverlay)
	if !ok {
		return fmt.Errorf("server seed overlay: sandbox %s does not expose a host workspace", s.sandbox.Name())
	}
	dest, err := hw.HostWorkspace(sbx)
	if err != nil {
		return fmt.Errorf("server seed overlay: %w", err)
	}
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("server seed overlay: sandbox %s returned an empty host workspace", s.sandbox.Name())
	}
	n, err := copyWorkspace(src, dest)
	if err != nil {
		return fmt.Errorf("server seed overlay: %w", err)
	}
	s.log.Info("sandbox overlay seeded from host checkout",
		zap.String("src", src),
		zap.String("dest", dest),
		zap.Int("files", n),
	)
	return nil
}

func copyWorkspace(src, dest string) (int, error) {
	src, err := filepath.Abs(src)
	if err != nil {
		return 0, fmt.Errorf("source: %w", err)
	}
	dest, err = filepath.Abs(dest)
	if err != nil {
		return 0, fmt.Errorf("dest: %w", err)
	}
	if src == dest {
		return 0, nil
	}
	files, err := listWorkspaceFiles(src)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rel := range files {
		rel = filepath.ToSlash(rel)
		if rel == "" || rel == "." || sandbox.ExcludedFromImport(rel) {
			continue
		}
		from := filepath.Join(src, filepath.FromSlash(rel))
		to := filepath.Join(dest, filepath.FromSlash(rel))
		if !withinDir(dest, to) {
			continue
		}
		info, err := os.Lstat(from)
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return n, fmt.Errorf("%s: %w", rel, err)
		}
		if err := copyFile(from, to, info.Mode()); err != nil {
			return n, fmt.Errorf("%s: %w", rel, err)
		}
		n++
	}
	return n, nil
}

func listWorkspaceFiles(root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		cmd := exec.Command("git", "-C", root, "ls-files", "-z")
		out, err := cmd.Output()
		if err == nil {
			return splitNull(out), nil
		}
	}
	var files []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			base := strings.ToLower(d.Name())
			if base == ".git" || base == "node_modules" {
				return filepath.SkipDir
			}
			if sandbox.ExcludedFromImport(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel != "." {
			files = append(files, rel)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("list workspace %s: %w", root, walkErr)
	}
	return files, nil
}

func splitNull(raw []byte) []string {
	parts := strings.Split(string(raw), "\x00")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func copyFile(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func withinDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
