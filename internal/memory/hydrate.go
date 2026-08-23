package memory

import (
	"bytes"
	"errors"
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
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("memory hydrate git dir: %w", err)
	}
	if !st.IsDir() {
		return nil
	}
	info := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		return fmt.Errorf("memory hydrate git info: %w", err)
	}
	excludePath := filepath.Join(info, "exclude")
	existing, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("memory hydrate git exclude: %w", err)
	}
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
		if err := skipWorktree(root, filepath.ToSlash(p)); err != nil {
			return err
		}
	}
	return nil
}

// skipWorktree marks a tracked compiled path so git status ignores the
// overlay rewrite. Untracked paths are covered by .git/info/exclude;
// git update-index --skip-worktree exits non-zero for those, and that
// is not a protection failure. A tracked path whose skip-worktree bit
// cannot be set is.
func skipWorktree(root, rel string) error {
	tracked, err := gitTracked(root, rel)
	if err != nil {
		return err
	}
	if !tracked {
		return nil
	}
	cmd := exec.Command("git", "-C", root, "update-index", "--skip-worktree", "--", rel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return gitCmdErr("skip-worktree", rel, out, err)
	}
	return nil
}

func gitTracked(root, rel string) (bool, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", rel)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, gitCmdErr("git ls-files", rel, out, err)
}

func gitCmdErr(op, rel string, out []byte, err error) error {
	msg := string(bytes.TrimSpace(out))
	if msg == "" {
		return fmt.Errorf("memory hydrate %s %s: %w", op, rel, err)
	}
	return fmt.Errorf("memory hydrate %s %s: %w: %s", op, rel, err, msg)
}
