package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/plan"
)

func TestApplyWorldExecuteWritesAndHashes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newApplyWorld(root)
	got, err := w.Observe(t.Context(), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("old\n"))
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("observe %q", got)
	}
	post, err := w.Execute(t.Context(), plan.Row{
		Op:             plan.OpModify,
		Target:         "README.md",
		Payload:        "new\n",
		IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new\n" {
		t.Fatalf("wrote %q", body)
	}
	want := contentHash([]byte("new\n"))
	if post != want {
		t.Fatalf("post %q want %q", post, want)
	}
	again, err := w.Execute(t.Context(), plan.Row{
		Op:             plan.OpModify,
		Target:         "README.md",
		Payload:        "other\n",
		IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again != post {
		t.Fatal("idempotency key did not replay")
	}
	body, _ = os.ReadFile(filepath.Join(root, "README.md"))
	if string(body) != "new\n" {
		t.Fatal("idempotent execute rewrote the file")
	}
	if len(w.targets()) != 1 || w.targets()[0] != "README.md" {
		t.Fatalf("targets %v", w.targets())
	}
}

func TestApplyWorldCreateAndDestroy(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	w := newApplyWorld(root)
	if _, err := w.Execute(t.Context(), plan.Row{
		Op:             plan.OpCreate,
		Target:         "docs/new.md",
		Payload:        "hello\n",
		IdempotencyKey: "c1",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "docs", "new.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("create %q", got)
	}
	post, err := w.Execute(t.Context(), plan.Row{
		Op:             plan.OpDestroy,
		Target:         "docs/new.md",
		Payload:        "-hello\n",
		IdempotencyKey: "d1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if post != emptyDigest() {
		t.Fatalf("destroy post %q", post)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "new.md")); !os.IsNotExist(err) {
		t.Fatalf("destroy left the file: %v", err)
	}
}

func TestApplyWorldObserveMissingIsEmpty(t *testing.T) {
	t.Parallel()
	w := newApplyWorld(t.TempDir())
	got, err := w.Observe(t.Context(), "missing.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("missing observe %q", got)
	}
}

func TestApplyWorldRejectsUnsafeTarget(t *testing.T) {
	t.Parallel()
	w := newApplyWorld(t.TempDir())
	_, err := w.Execute(t.Context(), plan.Row{
		Op:             plan.OpCreate,
		Target:         "../secret",
		Payload:        "nope",
		IdempotencyKey: "bad",
	})
	if err == nil {
		t.Fatal("expected unsafe target error")
	}
	if !strings.Contains(err.Error(), "unsafe target") {
		t.Fatalf("err %v", err)
	}
}

func TestApplyUnifiedDiffFromG4(t *testing.T) {
	t.Parallel()
	original := []byte("# demo\n")
	diff := "--- a/README.md\n+++ b/README.md\n@@ -1 +1,2 @@\n # demo\n+Version: 2\n"
	got, err := plan.ApplyPatch(original, diff)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# demo\nVersion: 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyRowUsesUnifiedDiff(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	post, err := applyRow(root, plan.Row{
		Op:      plan.OpModify,
		Target:  "README.md",
		Payload: "--- a/README.md\n+++ b/README.md\n@@ -1 +1,2 @@\n # demo\n+Version: 2\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# demo\nVersion: 2\n" {
		t.Fatalf("body %q", body)
	}
	if post != contentHash(body) {
		t.Fatalf("post %q", post)
	}
}

func TestApplyUnifiedDiffConflict(t *testing.T) {
	t.Parallel()
	_, err := plan.ApplyPatch([]byte("other\n"), "@@ -1 +1,2 @@\n # demo\n+Version: 2\n")
	if err == nil {
		t.Fatal("expected conflict")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err %v", err)
	}
}

func TestApplyModifyPreservesUntargetedREADMEContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	original := "# Zeroth\n\n## Why Zeroth?\nAgents work at machine speed. Humans keep control.\n\n## Layout\ncmd/ zerothd and zeroth\n\n## Develop\nYou need Go 1.27.\n\n## License\nMIT\n"
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := "--- a/README.md\n+++ b/README.md\n@@\n+## Connecting Linear (assign-to-Zeroth)\n+\n+Assign an issue to the agent identity.\n"
	post, err := applyRow(root, plan.Row{
		Op:      plan.OpModify,
		Target:  "README.md",
		Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, keep := range []string{"# Zeroth", "## Why Zeroth?", "Humans keep control.", "## Layout", "## Develop", "## License", "MIT"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("lost %q; file became:\n%s", keep, got)
		}
	}
	if !strings.Contains(got, "## Connecting Linear (assign-to-Zeroth)") {
		t.Fatalf("missing added section:\n%s", got)
	}
	if strings.HasPrefix(strings.TrimSpace(got), "--- a/README.md") || strings.Contains(got, "+## Connecting") {
		t.Fatalf("wrote the diff as the file:\n%s", got)
	}
	if post != contentHash(body) {
		t.Fatalf("post %q", post)
	}
	if contentHash(body) == contentHash([]byte(payload)) {
		t.Fatal("post hash is the payload, which means the file was replaced with the diff")
	}
}

func TestApplyModifyRefusesShrinkingOverwrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var old strings.Builder
	old.WriteString("# Zeroth\n\n")
	for i := 0; i < 80; i++ {
		old.WriteString("This is a real overview paragraph that must survive apply.\n")
	}
	old.WriteString("## License\nMIT\n")
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte(old.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := applyRow(root, plan.Row{
		Op:      plan.OpModify,
		Target:  "README.md",
		Payload: "## Connecting Linear\n\nAssign an issue.\n",
	})
	if err == nil {
		t.Fatal("expected shrinking overwrite to be refused")
	}
	got, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatal(errRead)
	}
	if string(got) != old.String() {
		t.Fatalf("refused apply still mutated the file: %q", got)
	}
}

func TestApplyRowPostconditionMismatchDoesNotWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := applyRow(root, plan.Row{
		Op:            plan.OpModify,
		Target:        "README.md",
		Payload:       "new\n",
		Postcondition: "deadbeef",
	})
	if err == nil {
		t.Fatal("expected postcondition mismatch")
	}
	if !strings.Contains(err.Error(), "postcondition") {
		t.Fatalf("err %v", err)
	}
	got, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatal(errRead)
	}
	if string(got) != "old\n" {
		t.Fatalf("mismatch still wrote %q", got)
	}
}

func TestGitBranchNameFollowsLinearConvention(t *testing.T) {
	t.Parallel()
	got := gitBranchName("42-50", "Apply executor is a stub: approving a plan never touches git")
	if got != "zeroth/42-50-apply-executor-is-a-stub-approving-a-plan-never-touche" &&
		!strings.HasPrefix(got, "zeroth/42-50-apply-executor-is-a-stub") {
		t.Fatalf("branch %q", got)
	}
	if !strings.HasPrefix(got, "zeroth/42-50-") {
		t.Fatalf("expected zeroth/42-50- prefix, got %q", got)
	}
}

func TestGitPublisherProducesRefAndPR(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	remote := t.TempDir()
	overlay := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(gitCommandEnv(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run(remote, "init", "--bare")
	run(repo, "init", "-b", "main")
	run(repo, "config", "user.email", "zeroth@local")
	run(repo, "config", "user.name", "zeroth")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "README.md")
	run(repo, "commit", "-m", "init")
	run(repo, "remote", "add", "origin", remote)

	if err := os.WriteFile(filepath.Join(overlay, "README.md"), []byte("applied\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(overlay, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "docs", "new.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var ghArgs [][]string
	p := &gitPublisher{
		git: runGit,
		gh: func(_ context.Context, args ...string) (string, error) {
			ghArgs = append(ghArgs, append([]string(nil), args...))
			return "https://github.com/avivl/zeroth/pull/99\n", nil
		},
		execer: newGitPublisher().execer,
	}
	ref, err := p.Publish(t.Context(), ApplyPublish{
		Repo:      repo,
		Workspace: overlay,
		Targets:   []string{"README.md", "docs/new.md"},
		Branch:    "zeroth/42-50-apply-executor-is-a-stub",
		Title:     "Apply executor is a stub",
		Body:      "Approved plan.",
		IssueKey:  "42-50",
		PlanHash:  "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.PullRequest != "https://github.com/avivl/zeroth/pull/99" {
		t.Fatalf("pr %q", ref.PullRequest)
	}
	if ref.Commit == "" {
		t.Fatalf("commit %q looks empty", ref.Commit)
	}
	if ref.Branch != "zeroth/42-50-apply-executor-is-a-stub" {
		t.Fatalf("branch %q", ref.Branch)
	}
	show := exec.Command("git", "--git-dir", remote, "show", "zeroth/42-50-apply-executor-is-a-stub:README.md")
	out, err := show.CombinedOutput()
	if err != nil {
		t.Fatalf("remote show: %v\n%s", err, out)
	}
	if string(out) != "applied\n" {
		t.Fatalf("remote README %q", out)
	}
	show = exec.Command("git", "--git-dir", remote, "show", "zeroth/42-50-apply-executor-is-a-stub:docs/new.md")
	out, err = show.CombinedOutput()
	if err != nil {
		t.Fatalf("remote show new: %v\n%s", err, out)
	}
	if string(out) != "hello\n" {
		t.Fatalf("remote new.md %q", out)
	}
	if len(ghArgs) == 0 {
		t.Fatal("gh pr create was not called")
	}
	joined := strings.Join(ghArgs[0], " ")
	if !strings.Contains(joined, "pr create") || !strings.Contains(joined, "zeroth/42-50-apply-executor-is-a-stub") {
		t.Fatalf("gh args %v", ghArgs[0])
	}
}

func TestClosePullRequestClosesOpenAndSkipsClosed(t *testing.T) {
	t.Parallel()
	var ghArgs [][]string
	p := &gitPublisher{
		gh: func(_ context.Context, args ...string) (string, error) {
			ghArgs = append(ghArgs, append([]string(nil), args...))
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "pr view") {
				return `{"state":"OPEN"}`, nil
			}
			return "", nil
		},
		execer: newGitPublisher().execer,
	}
	url := "https://github.com/avivl/zeroth/pull/48"
	if err := p.ClosePullRequest(t.Context(), url, "Retracted by Zeroth.\n\nbad patch\n"); err != nil {
		t.Fatal(err)
	}
	if len(ghArgs) != 2 {
		t.Fatalf("gh calls %v", ghArgs)
	}
	if !strings.Contains(strings.Join(ghArgs[1], " "), "pr close") {
		t.Fatalf("close args %v", ghArgs[1])
	}

	p.gh = func(_ context.Context, args ...string) (string, error) {
		if strings.Join(args, " ") == "pr view "+url+" --json state" {
			return `{"state":"CLOSED"}`, nil
		}
		t.Fatalf("unexpected gh %v", args)
		return "", nil
	}
	if err := p.ClosePullRequest(t.Context(), url, "again"); err != nil {
		t.Fatal(err)
	}
}

func TestClosePullRequestRejectsMerged(t *testing.T) {
	t.Parallel()
	p := &gitPublisher{
		gh: func(_ context.Context, args ...string) (string, error) {
			return `{"state":"MERGED"}`, nil
		},
		execer: newGitPublisher().execer,
	}
	err := p.ClosePullRequest(t.Context(), "https://github.com/avivl/zeroth/pull/1", "nope")
	if !errors.Is(err, errPRMerged) {
		t.Fatalf("err %v, want errPRMerged", err)
	}
}

func TestGithubRepoSlug(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"git@github.com:avivl/zeroth.git", "avivl/zeroth", true},
		{"https://github.com/avivl/zeroth.git", "avivl/zeroth", true},
		{"https://example.com/r.git", "", false},
	}
	for _, tc := range cases {
		got, ok := githubRepoSlug(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("%s: got %q %v", tc.in, got, ok)
		}
	}
}
