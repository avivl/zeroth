package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHydrateSkipWorktreeFailureIsSurfaced(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zeroth", "GIT_AUTHOR_EMAIL=zeroth@example.com",
			"GIT_COMMITTER_NAME=zeroth", "GIT_COMMITTER_EMAIL=zeroth@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	run("config", "user.email", "zeroth@example.com")
	run("config", "user.name", "zeroth")
	if err := os.WriteFile(filepath.Join(root, CompiledAgents), []byte("# source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", CompiledAgents)
	run("commit", "-m", "init")

	bin := t.TempDir()
	wrapper := filepath.Join(bin, "git")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = \"--skip-worktree\" ]; then\n" +
		"    echo simulated skip-worktree failure >&2\n" +
		"    exit 1\n" +
		"  fi\n" +
		"done\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	b := NewBook()
	if _, err := b.Write(Human("alice"), KindOperator, "", "note", "compiled overlay", "operator"); err != nil {
		t.Fatal(err)
	}
	err = Hydrate(root, b.Slice())
	if err == nil {
		t.Fatal("hydrate succeeded despite skip-worktree failure")
	}
	got := err.Error()
	if !strings.Contains(got, "skip-worktree") {
		t.Fatalf("error %q does not name skip-worktree", got)
	}
	if !strings.Contains(got, "simulated skip-worktree failure") && !strings.Contains(got, "exit status 1") {
		t.Fatalf("error %q does not surface the git failure", got)
	}
}

func TestHydrateGitExcludeWriteFailureIsSurfaced(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "info"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Hydrate(root, nil)
	if err == nil {
		t.Fatal("hydrate succeeded despite git info exclude path not being a directory")
	}
	if !strings.Contains(err.Error(), "git info") {
		t.Fatalf("error %q does not name git info", err)
	}
}
