package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompileMatchesSliceAndDeleteDropsKey(t *testing.T) {
	t.Parallel()
	b := NewBook()
	b.now = func() time.Time { return time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC) }
	if _, err := b.Write(Human("alice"), KindOperator, "", "style.tests", "prefer table tests", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write(Human("alice"), KindOperator, "", "kernel.rule", "policy outranks the harness", "operator"); err != nil {
		t.Fatal(err)
	}
	slice := b.Slice()
	compiled := Compile(slice)
	if compiled != Compile(slice) {
		t.Fatal("compile is not deterministic")
	}
	keys := KeysInCompile(compiled)
	if len(keys) != 2 || keys[0] != "kernel.rule" || keys[1] != "style.tests" {
		t.Fatalf("keys %v", keys)
	}
	if !strings.Contains(compiled, "prefer table tests") || !strings.Contains(compiled, "policy outranks the harness") {
		t.Fatalf("compiled %s", compiled)
	}
	root := t.TempDir()
	if err := Hydrate(root, slice); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, CompiledAgents))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != compiled {
		t.Fatalf("hydrated AGENTS.md does not match compile\n got %q\nwant %q", got, compiled)
	}
	claude, err := os.ReadFile(filepath.Join(root, CompiledClaude))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claude), "prefer table tests") {
		t.Fatalf("CLAUDE.md missing facts: %s", claude)
	}

	if _, err := b.Delete(Human("alice"), KindOperator, "", "style.tests", "operator"); err != nil {
		t.Fatal(err)
	}
	next := Compile(b.Slice())
	if strings.Contains(next, "style.tests") || strings.Contains(next, "prefer table tests") {
		t.Fatalf("deleted fact still compiled: %s", next)
	}
	if !strings.Contains(next, "kernel.rule") {
		t.Fatal("remaining fact missing from compile")
	}
	if err := Hydrate(root, b.Slice()); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(root, CompiledAgents))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != next {
		t.Fatal("re-hydrate did not match next compile")
	}
}

func TestHydrateExcludesCompiledFilesFromGit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=zeroth", "GIT_AUTHOR_EMAIL=zeroth@example.com",
			"GIT_COMMITTER_NAME=zeroth", "GIT_COMMITTER_EMAIL=zeroth@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	run("config", "user.email", "zeroth@example.com")
	run("config", "user.name", "zeroth")
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, CompiledAgents), []byte("# source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README", CompiledAgents)
	run("commit", "-m", "init")

	b := NewBook()
	if _, err := b.Write(Human("alice"), KindOperator, "", "note", "compiled overlay", "operator"); err != nil {
		t.Fatal(err)
	}
	if err := Hydrate(root, b.Slice()); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, CompiledAgents))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "compiled overlay") {
		t.Fatal("hydrate did not overwrite AGENTS.md")
	}
	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exclude), CompiledAgents) {
		t.Fatalf("exclude missing AGENTS.md: %s", exclude)
	}
	status := exec.Command("git", "status", "--porcelain")
	status.Dir = root
	out, err := status.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), CompiledAgents) || strings.Contains(string(out), CompiledClaude) {
		t.Fatalf("compiled files appear in git status: %s", out)
	}
}
