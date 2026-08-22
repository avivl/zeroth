package memory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const excludeMarker = "# zeroth compiled memory (Z1-118)"

// Hydrate writes compiled harness files into root and keeps them out of
// git when root is a repository. Call this at session start and after
// restoring a checkpoint so the overlay matches the notebook slice.
func Hydrate(root string, facts []Fact) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("memory hydrate: empty root: %w", ErrInvalid)
	}
	files := CompileAll(facts)
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("memory hydrate mkdir %s: %w", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("memory hydrate write %s: %w", rel, err)
		}
	}
	if err := protectFromCommit(root); err != nil {
		return err
	}
	return nil
}

func protectFromCommit(root string) error {
	gitDir := filepath.Join(root, ".git")
	st, err := os.Stat(gitDir)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		return nil
	}
	info := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		return fmt.Errorf("memory hydrate git info: %w", err)
	}
	excludePath := filepath.Join(info, "exclude")
	existing, _ := os.ReadFile(excludePath)
	text := string(existing)
	if !strings.Contains(text, excludeMarker) {
		var bld strings.Builder
		if len(text) > 0 && !strings.HasSuffix(text, "\n") {
			bld.WriteString(text)
			bld.WriteByte('\n')
		} else {
			bld.WriteString(text)
		}
		bld.WriteString(excludeMarker)
		bld.WriteByte('\n')
		for _, p := range CompiledPaths() {
			bld.WriteString(p)
			bld.WriteByte('\n')
		}
		bld.WriteString(CompiledMarkerDir + "/\n")
		if err := os.WriteFile(excludePath, []byte(bld.String()), 0o644); err != nil {
			return fmt.Errorf("memory hydrate git exclude: %w", err)
		}
	}
	for _, p := range CompiledPaths() {
		cmd := exec.Command("git", "-C", root, "update-index", "--skip-worktree", "--", filepath.ToSlash(p))
		_ = cmd.Run()
	}
	return nil
}
