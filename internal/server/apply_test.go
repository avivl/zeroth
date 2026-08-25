package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
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

func TestClosePullRequestEmptyURL(t *testing.T) {
	t.Parallel()
	p := &gitPublisher{
		gh:     func(context.Context, ...string) (string, error) { return "", nil },
		execer: newGitPublisher().execer,
	}
	if err := p.ClosePullRequest(t.Context(), "  ", "nope"); err == nil {
		t.Fatal("expected empty url error")
	}
}

func TestClosePullRequestTreatsGhAlreadyClosedAsSuccess(t *testing.T) {
	t.Parallel()
	p := &gitPublisher{
		gh: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "pr view") {
				return `{"state":"OPEN"}`, nil
			}
			return "", errors.New("GraphQL: Pull request is already closed")
		},
		execer: newGitPublisher().execer,
	}
	if err := p.ClosePullRequest(t.Context(), "https://github.com/avivl/zeroth/pull/2", ""); err != nil {
		t.Fatal(err)
	}
}

func TestClosePullRequestAlreadyClosedMessage(t *testing.T) {
	t.Parallel()
	p := &gitPublisher{
		gh: func(_ context.Context, args ...string) (string, error) {
			if strings.Contains(strings.Join(args, " "), "pr view") {
				return `{"state":"OPEN"}`, nil
			}
			return "", errors.New("GraphQL: already closed")
		},
		execer: newGitPublisher().execer,
	}
	if err := p.ClosePullRequest(t.Context(), "https://github.com/avivl/zeroth/pull/1", ""); err != nil {
		t.Fatal(err)
	}
}

func TestClosePullRequestTreatsGhAlreadyMergedAsConflict(t *testing.T) {
	t.Parallel()
	p := &gitPublisher{
		gh: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "pr view") {
				return "not-json", nil
			}
			return "", errors.New("this pull request was merged")
		},
		execer: newGitPublisher().execer,
	}
	err := p.ClosePullRequest(t.Context(), "https://github.com/avivl/zeroth/pull/3", "nope")
	if !errors.Is(err, errPRMerged) {
		t.Fatalf("err %v, want errPRMerged", err)
	}
}

func TestClosePullRequestGenericError(t *testing.T) {
	t.Parallel()
	p := &gitPublisher{
		gh: func(_ context.Context, args ...string) (string, error) {
			if strings.Contains(strings.Join(args, " "), "pr view") {
				return `{"state":"OPEN"}`, nil
			}
			return "", errors.New("rate limited")
		},
		execer: newGitPublisher().execer,
	}
	if err := p.ClosePullRequest(t.Context(), "https://github.com/avivl/zeroth/pull/1", "x"); err == nil {
		t.Fatal("expected close error")
	}
}

func TestRetractHelpers(t *testing.T) {
	t.Parallel()
	if got := retractPostcondition(false, ""); got != "retracted" {
		t.Fatalf("empty pr: %s", got)
	}
	if got := retractPostcondition(true, "https://example.com/pr/1"); got != "pr_closed" {
		t.Fatalf("closed: %s", got)
	}
	if got := retractPostcondition(false, "https://example.com/pr/1"); got != "retracted" {
		t.Fatalf("open pr: %s", got)
	}
	if got := firstNonEmpty("", "  ", "s_1"); got != "s_1" {
		t.Fatalf("firstNonEmpty: %s", got)
	}
	if got := firstNonEmpty("", " "); got != "" {
		t.Fatalf("all empty: %q", got)
	}
	if got := retractPRComment("", "bad patch"); strings.Contains(got, "run `") {
		t.Fatalf("empty run id should omit run clause: %s", got)
	}
	if got := retractPRComment("s_9", "bad patch"); !strings.Contains(got, "run `s_9`") {
		t.Fatalf("comment: %s", got)
	}
	status, code, _ := statusForRetractError(errPRMerged)
	if status != http.StatusConflict || code != "conflict" {
		t.Fatalf("merged %d %s", status, code)
	}
	status, _, _ = statusForRetractError(errors.New("pull request is already merged"))
	if status != http.StatusConflict {
		t.Fatalf("merged string %d", status)
	}
}

func TestClosePullRequestViewError(t *testing.T) {
	t.Parallel()
	p := &gitPublisher{
		gh: func(context.Context, ...string) (string, error) {
			return "", errors.New("gh: not found")
		},
		execer: newGitPublisher().execer,
	}
	if err := p.ClosePullRequest(t.Context(), "https://github.com/avivl/zeroth/pull/4", "x"); err == nil {
		t.Fatal("expected view error")
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

func TestPeekPRFallsBackToStore(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := New(Config{Store: st, TokenInterval: time.Hour, TokenCount: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	aid, err := store.ParseAgentID(DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := store.ParseSessionID("s_retract_store")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	url := "https://github.com/avivl/zeroth/pull/48"
	if err := st.CreateSession(t.Context(), store.Session{
		ID:          sid,
		AgentID:     aid,
		Status:      "done",
		PullRequest: url,
		CreatedAt:   now,
		UpdatedAt:   now,
		FinishedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}
	if got := srv.peekPR(sid.String()); got != url {
		t.Fatalf("peekPR from store = %q", got)
	}
	srv.rememberPR("", url)
	srv.rememberPR(sid.String(), "")
	if got := srv.peekPR("not-an-id"); got != "" {
		t.Fatalf("invalid id peekPR = %q", got)
	}
}

type stubApplyPublisher struct {
	calls []ApplyPublish
	ref   ApplyRef
}

func (p *stubApplyPublisher) Publish(_ context.Context, req ApplyPublish) (ApplyRef, error) {
	p.calls = append(p.calls, req)
	ref := p.ref
	if ref.PullRequest == "" {
		ref = ApplyRef{Branch: req.Branch, Commit: "sha", PullRequest: "https://github.com/avivl/zeroth/pull/1"}
	}
	return ref, nil
}

func (p *stubApplyPublisher) ClosePullRequest(context.Context, string, string) error { return nil }

func TestPublishAppliedEmptyFileEffectsIsAnError(t *testing.T) {
	t.Parallel()
	pub := &stubApplyPublisher{}
	srv, sess, sid := publishTestServer(t, pub)
	p := fileEffectPlan(t, "CHANGELOG.md")
	err := srv.publishApplied(t.Context(), sess, sid, p, newApplyWorld(t.TempDir()), t.TempDir())
	if err == nil {
		t.Fatal("expected empty-targets file effects to fail")
	}
	if !strings.Contains(err.Error(), "no workspace targets were written") {
		t.Fatalf("err %v", err)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher ran: %+v", pub.calls)
	}
	if rec := lastPublishAudit(t, srv); rec.Action != "" {
		t.Fatalf("failed empty publish still audited: %+v", rec)
	}
}

func TestPublishAppliedMemoryOnlyRecordsNothingToPublish(t *testing.T) {
	t.Parallel()
	pub := &stubApplyPublisher{}
	srv, sess, sid := publishTestServer(t, pub)
	p := plan.Plan{
		ID:      mustPlanID(t, "p_mem_only"),
		Hash:    "hash-mem",
		Summary: "remember a style",
		Rows:    []plan.Row{{Op: plan.OpMemoryProposal, Target: "session/style", Payload: "prefer table tests"}},
	}
	if err := srv.publishApplied(t.Context(), sess, sid, p, newApplyWorld(t.TempDir()), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 0 {
		t.Fatal("memory-only publish must not open a PR")
	}
	rec := lastPublishAudit(t, srv)
	if rec.Action != audit.ActionPlanApplyPublish || rec.Postcondition != publishOutcomeNothingToPublish {
		t.Fatalf("audit %+v", rec)
	}
}

func TestPublishAppliedEmptyTargetsWithExistingPRIsAlreadyPublished(t *testing.T) {
	t.Parallel()
	pub := &stubApplyPublisher{}
	srv, sess, sid := publishTestServer(t, pub)
	url := "https://github.com/avivl/zeroth/pull/7"
	srv.rememberPR(sess.ID.String(), url)
	p := fileEffectPlan(t, "CHANGELOG.md")
	if err := srv.publishApplied(t.Context(), sess, sid, p, newApplyWorld(t.TempDir()), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 0 {
		t.Fatal("retry with an existing PR must not publish again")
	}
	rec := lastPublishAudit(t, srv)
	if rec.Postcondition != publishOutcomeAlreadyPublished || rec.Target != url {
		t.Fatalf("audit %+v", rec)
	}
}

func TestPublishAppliedRecordsPublishedOutcome(t *testing.T) {
	t.Parallel()
	pub := &stubApplyPublisher{}
	srv, sess, sid := publishTestServer(t, pub)
	root := t.TempDir()
	world := newApplyWorld(root)
	row := plan.Row{Op: plan.OpCreate, Target: "CHANGELOG.md", Payload: "# Changelog\n", IdempotencyKey: "k"}
	if _, err := world.Execute(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	p := fileEffectPlan(t, "CHANGELOG.md")
	if err := srv.publishApplied(t.Context(), sess, sid, p, world, root); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 1 || len(pub.calls[0].Targets) != 1 || pub.calls[0].Targets[0] != "CHANGELOG.md" {
		t.Fatalf("publish %+v", pub.calls)
	}
	rec := lastPublishAudit(t, srv)
	if rec.Postcondition != publishOutcomePublished {
		t.Fatalf("audit %+v", rec)
	}
	if srv.peekPR(sess.ID.String()) != "https://github.com/avivl/zeroth/pull/1" {
		t.Fatalf("pr %q", srv.peekPR(sess.ID.String()))
	}
}

func publishTestServer(t *testing.T, pub ApplyPublisher) (*Server, store.Session, session.ID) {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := New(Config{Store: st, TokenInterval: time.Hour, TokenCount: 8, Publisher: pub})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	aid, err := store.ParseAgentID(DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	sidStore, err := store.ParseSessionID("s_publish_empty")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sess := store.Session{ID: sidStore, AgentID: aid, Status: "applying", CreatedAt: now, UpdatedAt: now}
	if err := st.CreateSession(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	sid, err := session.ParseID(sess.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	return srv, sess, sid
}

func fileEffectPlan(t *testing.T, target string) plan.Plan {
	t.Helper()
	return plan.Plan{
		ID:      mustPlanID(t, "p_file_effect"),
		Hash:    "hash-file",
		Summary: "create " + target,
		Rows:    []plan.Row{{Op: plan.OpCreate, Target: target, Payload: "# Changelog\n"}},
	}
}

func mustPlanID(t *testing.T, raw string) plan.ID {
	t.Helper()
	id, err := plan.ParseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func lastPublishAudit(t *testing.T, srv *Server) store.AuditRecord {
	t.Helper()
	chain, err := srv.store.AuditChain(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].Action == audit.ActionPlanApplyPublish {
			return chain[i]
		}
	}
	return store.AuditRecord{}
}
