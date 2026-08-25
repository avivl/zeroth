package server_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"github.com/avivl/zeroth/internal/tracker"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"go.uber.org/zap"
)

type recordingPublisher struct {
	mu           sync.Mutex
	req          server.ApplyPublish
	calls        []server.ApplyPublish
	files        map[string]string
	ref          server.ApplyRef
	emptyPR      bool
	closed       []string
	closeComment string
	closeErr     error
}

func (p *recordingPublisher) Publish(_ context.Context, req server.ApplyPublish) (server.ApplyRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.req = req
	p.calls = append(p.calls, req)
	p.files = make(map[string]string, len(req.Targets))
	for _, target := range req.Targets {
		body, err := os.ReadFile(filepath.Join(req.Workspace, filepath.FromSlash(target)))
		if err != nil {
			p.files[target] = ""
			continue
		}
		p.files[target] = string(body)
	}
	if p.emptyPR {
		ref := server.ApplyRef{Branch: req.Branch, Commit: "testsha"}
		p.ref = ref
		return ref, nil
	}
	ref := p.ref
	if ref.PullRequest == "" {
		ref = server.ApplyRef{
			Branch:      req.Branch,
			Commit:      "testsha",
			PullRequest: "https://github.com/avivl/zeroth/pull/99",
		}
	}
	if ref.Branch == "" {
		ref.Branch = req.Branch
	}
	p.ref = ref
	return ref, nil
}

func (p *recordingPublisher) ClosePullRequest(_ context.Context, url, comment string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = append(p.closed, url)
	p.closeComment = comment
	if p.closeErr != nil {
		return p.closeErr
	}
	return nil
}

type applyEnv struct {
	examEnv
	pub  *recordingPublisher
	sbx  *fakeSandbox
	root string
	tr   *stubTracker
}

func applySetup(t *testing.T) *applyEnv {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return applySetupOn(t, st, nil)
}

func applySetupOn(t *testing.T, st store.Store, log *zap.Logger) *applyEnv {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "design"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "design", "plan.md"), []byte("typo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rev := &capturingReviewer{}
	pub := &recordingPublisher{}
	sbx := newFakeSandbox()
	tr := newStubTracker(tracker.Issue{Key: "42-50", Title: "Apply executor is a stub"})
	srv, err := server.New(server.Config{
		Store:         st,
		Log:           log,
		Reviewer:      rev,
		TokenInterval: time.Hour,
		TokenCount:    1000,
		Sandbox:       sbx,
		WorkspaceRoot: root,
		Publisher:     pub,
		Tracker:       tr,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	e := &applyEnv{
		examEnv: examEnv{t: t, st: st, srv: srv, hs: hs, rev: rev},
		pub:     pub,
		sbx:     sbx,
		root:    root,
		tr:      tr,
	}
	return e
}

func (e *applyEnv) seedPatchedFilePlan(run gen.Run, effects []plan.Proposed) store.PlanID {
	e.t.Helper()
	observed := make(map[string]string)
	bodies := make(map[string]string)
	for _, fx := range effects {
		if fx.Type != "modify" && fx.Type != "destroy" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(e.root, filepath.FromSlash(fx.Path)))
		if err != nil {
			e.t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		observed[fx.Path] = hex.EncodeToString(sum[:])
		bodies[fx.Path] = string(body)
	}
	return e.seedPlanWithBodies(run, effects, observed, bodies)
}

func TestGoldenApproveAndApplyOverHTTP(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	pid := e.seedPatchedFilePlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	})
	if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
		t.Fatal(err)
	}

	inbox, err := http.Get(e.hs.URL + "/approvals?status=pending")
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Body.Close()
	if inbox.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(inbox.Body)
		t.Fatalf("inbox %d %s", inbox.StatusCode, slurp)
	}
	var approvals gen.ApprovalList
	if err := json.NewDecoder(inbox.Body).Decode(&approvals); err != nil {
		t.Fatal(err)
	}
	if len(approvals.Items) != 1 || approvals.Items[0].PlanId == nil || string(*approvals.Items[0].PlanId) != pid.String() {
		t.Fatalf("inbox %+v", approvals.Items)
	}

	comment := "ship it"
	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("approve %d %s", res.StatusCode, slurp)
	}
	var approved gen.Plan
	if err := json.NewDecoder(res.Body).Decode(&approved); err != nil {
		t.Fatal(err)
	}
	if approved.Status != gen.PlanStatusApproved {
		t.Fatalf("status %s", approved.Status)
	}

	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	defer applied.Body.Close()
	if applied.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(applied.Body)
		t.Fatalf("apply %d %s", applied.StatusCode, slurp)
	}
	var out gen.ApplyPlanResponse
	if err := json.NewDecoder(applied.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Plan.Status != gen.PlanStatusApplied {
		t.Fatalf("applied status %s", out.Plan.Status)
	}
	if out.AuditId == "" {
		t.Fatal("missing apply audit id")
	}
	e.pub.mu.Lock()
	if e.pub.files["docs/design/plan.md"] != "fixed\n" {
		t.Fatalf("applied patch not written: %+v", e.pub.files)
	}
	if e.pub.req.Branch == "" || strings.HasPrefix(e.pub.ref.PullRequest, "applied:") {
		t.Fatalf("publisher stub still returned in-memory apply string: %+v", e.pub.ref)
	}
	if e.pub.ref.PullRequest != "https://github.com/avivl/zeroth/pull/99" {
		t.Fatalf("pr %q", e.pub.ref.PullRequest)
	}
	if !strings.Contains(e.pub.req.Branch, "zeroth/") {
		t.Fatalf("branch %q", e.pub.req.Branch)
	}
	e.pub.mu.Unlock()

	verify := postJSON(t, e.hs.URL+"/audit/"+string(out.AuditId)+"/verify", struct{}{})
	defer verify.Body.Close()
	if verify.StatusCode != http.StatusOK {
		t.Fatalf("verify %d", verify.StatusCode)
	}
	var vr gen.AuditVerification
	if err := json.NewDecoder(verify.Body).Decode(&vr); err != nil {
		t.Fatal(err)
	}
	if !vr.Valid {
		t.Fatalf("signature invalid: %v", vr.Reason)
	}

	trail, err := http.Get(e.hs.URL + "/audit?resource_type=plan")
	if err != nil {
		t.Fatal(err)
	}
	defer trail.Body.Close()
	if trail.StatusCode != http.StatusOK {
		t.Fatalf("list plan audit %d", trail.StatusCode)
	}
	var planAudit gen.AuditList
	if err := json.NewDecoder(trail.Body).Decode(&planAudit); err != nil {
		t.Fatal(err)
	}
	var rowRec *gen.AuditRecord
	for i := range planAudit.Items {
		if planAudit.Items[i].Action == "plan.apply.row" {
			rowRec = &planAudit.Items[i]
			break
		}
	}
	if rowRec == nil {
		t.Fatal("apply did not sign a plan.apply.row record")
	}
	if rowRec.Signature == "" {
		t.Fatal("row signature is empty")
	}
	if rowRec.Id == out.AuditId {
		t.Fatal("row signature is the plan-level apply record")
	}
	if rowRec.Target == nil || *rowRec.Target != "docs/design/plan.md" {
		t.Fatalf("row target %+v", rowRec.Target)
	}
	rowVerify := postJSON(t, e.hs.URL+"/audit/"+string(rowRec.Id)+"/verify", struct{}{})
	defer rowVerify.Body.Close()
	if rowVerify.StatusCode != http.StatusOK {
		t.Fatalf("row verify %d", rowVerify.StatusCode)
	}
	var rowVR gen.AuditVerification
	if err := json.NewDecoder(rowVerify.Body).Decode(&rowVR); err != nil {
		t.Fatal(err)
	}
	if !rowVR.Valid {
		t.Fatalf("row signature invalid: %v", rowVR.Reason)
	}

	mem := postJSON(t, e.hs.URL+"/memory", gen.CreateMemoryRequest{
		Kind:    gen.MemoryKindOperator,
		Content: "prefer docs/ edits",
	})
	defer mem.Body.Close()
	if mem.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(mem.Body)
		t.Fatalf("memory %d %s", mem.StatusCode, slurp)
	}

	ck := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/checkpoints", gen.CreateCheckpointRequest{})
	// The run is terminal after apply (sandbox released), so an
	// on-demand checkpoint may 409.
	ck.Body.Close()
	if ck.StatusCode != http.StatusCreated && ck.StatusCode != http.StatusConflict {
		t.Fatalf("checkpoint %d", ck.StatusCode)
	}

	leases, err := http.Get(e.hs.URL + "/agents/" + server.DefaultAgentID + "/leases")
	if err != nil {
		t.Fatal(err)
	}
	defer leases.Body.Close()
	if leases.StatusCode != http.StatusOK {
		t.Fatalf("leases %d", leases.StatusCode)
	}
	var list gen.LeaseList
	if err := json.NewDecoder(leases.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("apply should release leases, still have %d", len(list.Items))
	}
}

func TestApplyPreconditionMismatchFailsClosed(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	pid := e.seedPatchedFilePlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	})
	if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
		t.Fatal(err)
	}
	comment := "ship it"
	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("approve %d", res.StatusCode)
	}

	overlay, err := e.sbx.HostWorkspace(e.sbx.anyID())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "docs", "design", "plan.md"), []byte("changed since draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	defer applied.Body.Close()
	if applied.StatusCode != http.StatusConflict {
		slurp, _ := io.ReadAll(applied.Body)
		t.Fatalf("apply %d %s, want conflict", applied.StatusCode, slurp)
	}
	slurp, _ := io.ReadAll(applied.Body)
	if !strings.Contains(string(slurp), "precondition drift") && !strings.Contains(string(slurp), "stale") {
		t.Fatalf("want a clear stale/drift error, got %s", slurp)
	}
	e.pub.mu.Lock()
	defer e.pub.mu.Unlock()
	if e.pub.req.Workspace != "" {
		t.Fatal("publisher ran after a precondition mismatch")
	}
}

func TestApplyCompletionCommentIncludesPullRequest(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)
	e.tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-50", Issue: e.tr.issue, At: time.Now()}
	run := waitRunByTracker(t, e.hs.URL, "42-50")
	pid := e.seedPatchedFilePlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	})
	if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
		t.Fatal(err)
	}
	comment := "ship it"
	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("approve %d", res.StatusCode)
	}
	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	defer applied.Body.Close()
	if applied.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(applied.Body)
		t.Fatalf("apply %d %s", applied.StatusCode, slurp)
	}

	found := false
	for _, c := range e.tr.commentBodies() {
		if strings.Contains(c, "### Zeroth completed") && strings.Contains(c, "https://github.com/avivl/zeroth/pull/99") {
			found = true
		}
		if strings.Contains(c, "Pull request: none") || strings.Contains(c, "| Pull request | none |") {
			t.Fatalf("completion still reports none: %s", c)
		}
	}
	if !found {
		t.Fatalf("missing PR link in completion: %v", e.tr.commentBodies())
	}
	if urls := e.tr.artifactURLs(); len(urls) == 0 || urls[0] != "https://github.com/avivl/zeroth/pull/99" {
		t.Fatalf("artifacts %v", urls)
	}
}

func TestIdenticalCreatesOnTwoRunsEachOpenPR(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)
	effects := []plan.Proposed{{Type: "create", Path: "CHANGELOG.md", Diff: "# Changelog\n"}}
	for i := 0; i < 2; i++ {
		run := createRun(t, e.hs.URL, "Create a CHANGELOG.md stub")
		pid := e.seedPlan(run, effects, nil)
		if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
			t.Fatal(err)
		}
		comment := "ship it"
		res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("run %d approve %d", i, res.StatusCode)
		}
		applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
		slurp, _ := io.ReadAll(applied.Body)
		applied.Body.Close()
		if applied.StatusCode != http.StatusOK {
			t.Fatalf("run %d apply %d %s", i, applied.StatusCode, slurp)
		}
		var out gen.ApplyPlanResponse
		if err := json.Unmarshal(slurp, &out); err != nil {
			t.Fatal(err)
		}
		if out.Plan.Status != gen.PlanStatusApplied {
			t.Fatalf("run %d status %s", i, out.Plan.Status)
		}
	}
	e.pub.mu.Lock()
	defer e.pub.mu.Unlock()
	if len(e.pub.calls) != 2 {
		t.Fatalf("publisher calls %d, want 2 independent PRs: %+v", len(e.pub.calls), e.pub.calls)
	}
	for i, req := range e.pub.calls {
		if len(req.Targets) != 1 || req.Targets[0] != "CHANGELOG.md" {
			t.Fatalf("call %d targets %v", i, req.Targets)
		}
		if e.pub.files["CHANGELOG.md"] != "# Changelog\n" && req.Workspace == "" {
			t.Fatalf("call %d missing changelog payload: %+v", i, req)
		}
	}
}

func TestApplyWithoutPullRequestDoesNotCompleteTracker(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)
	e.pub.emptyPR = true
	e.tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-50", Issue: e.tr.issue, At: time.Now()}
	run := waitRunByTracker(t, e.hs.URL, "42-50")
	pid := e.seedPatchedFilePlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	})
	if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
		t.Fatal(err)
	}
	comment := "ship it"
	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("approve %d", res.StatusCode)
	}
	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	slurp, _ := io.ReadAll(applied.Body)
	applied.Body.Close()
	if applied.StatusCode == http.StatusOK {
		t.Fatalf("apply without a PR reported success: %s", slurp)
	}
	if !strings.Contains(string(slurp), "no pull request") {
		t.Fatalf("want a visible missing-PR error, got %s", slurp)
	}
	if e.tr.lastState() == tracker.StateCompleted {
		t.Fatal("tracker moved to In Review without a PR")
	}
	foundFail := false
	for _, c := range e.tr.commentBodies() {
		if strings.Contains(c, "### Zeroth completed") {
			t.Fatalf("completion comment without a PR: %s", c)
		}
		if strings.Contains(c, "### Zeroth failed") {
			foundFail = true
		}
	}
	if !foundFail {
		t.Fatalf("missing fail comment: %v", e.tr.commentBodies())
	}
}

func TestApplyModifyPreservesExistingREADMEOverHTTP(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)
	original := "# Zeroth\n\n## Why Zeroth?\nAgents work at machine speed. Humans keep control.\n\n## Layout\ncmd/ zerothd and zeroth\n\n## Develop\nYou need Go 1.27.\n\n## License\nMIT\n"
	if err := os.WriteFile(filepath.Join(e.root, "README.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	run := createRun(t, e.hs.URL, "Document Linear assignment in README.md")
	payload := "--- a/README.md\n+++ b/README.md\n@@\n+## Connecting Linear (assign-to-Zeroth)\n+\n+Assign an issue to the agent identity.\n"
	pid := e.seedPatchedFilePlan(run, []plan.Proposed{
		{Type: "modify", Path: "README.md", Diff: payload},
	})
	if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
		t.Fatal(err)
	}
	comment := "ship it"
	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("approve %d", res.StatusCode)
	}
	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	defer applied.Body.Close()
	if applied.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(applied.Body)
		t.Fatalf("apply %d %s", applied.StatusCode, slurp)
	}
	e.pub.mu.Lock()
	got := e.pub.files["README.md"]
	e.pub.mu.Unlock()
	for _, keep := range []string{"# Zeroth", "## Why Zeroth?", "Humans keep control.", "## Layout", "## Develop", "## License", "MIT"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("lost %q; applied file:\n%s", keep, got)
		}
	}
	if !strings.Contains(got, "## Connecting Linear (assign-to-Zeroth)") {
		t.Fatalf("missing added section:\n%s", got)
	}
	if strings.HasPrefix(strings.TrimSpace(got), "--- a/README.md") {
		t.Fatalf("wrote the diff as the file:\n%s", got)
	}
}

func TestApplyPostconditionMismatchFailsLoudly(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)
	e.tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-50", Issue: e.tr.issue, At: time.Now()}
	run := waitRunByTracker(t, e.hs.URL, "42-50")
	pid := e.seedPlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	}, map[string]string{"docs/design/plan.md": func() string {
		sum := sha256.Sum256([]byte("typo\n"))
		return hex.EncodeToString(sum[:])
	}()})
	if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
		t.Fatal(err)
	}
	comment := "ship it"
	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("approve %d", res.StatusCode)
	}
	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	defer applied.Body.Close()
	if applied.StatusCode != http.StatusConflict && applied.StatusCode != http.StatusInternalServerError {
		slurp, _ := io.ReadAll(applied.Body)
		t.Fatalf("apply %d %s, want failure", applied.StatusCode, slurp)
	}
	slurp, _ := io.ReadAll(applied.Body)
	if !strings.Contains(string(slurp), "postcondition") {
		t.Fatalf("want postcondition mismatch, got %s", slurp)
	}
	e.pub.mu.Lock()
	if e.pub.req.Workspace != "" {
		t.Fatal("publisher ran after a postcondition mismatch")
	}
	e.pub.mu.Unlock()
	found := false
	for _, c := range e.tr.commentBodies() {
		if strings.Contains(c, "### Zeroth failed") && strings.Contains(c, "postcondition") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing Zeroth failed comment: %v", e.tr.commentBodies())
	}
}

func TestRequestChangesAndBranch(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	e.patchReviewer(false)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	pid := e.seedPlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	}, map[string]string{"docs/design/plan.md": "pre"})
	if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
		t.Fatal(err)
	}

	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/request-changes", gen.RequestChangesRequest{Comment: "narrow the diff"})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("request-changes %d %s", res.StatusCode, slurp)
	}
	var changed gen.Plan
	if err := json.NewDecoder(res.Body).Decode(&changed); err != nil {
		t.Fatal(err)
	}
	if changed.Status != gen.PlanStatusChangesRequested {
		t.Fatalf("status %s", changed.Status)
	}
	if changed.ReviewComment == nil || *changed.ReviewComment != "narrow the diff" {
		t.Fatalf("review_comment %+v", changed.ReviewComment)
	}

	note := "safer alternative"
	br := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/branch", gen.BranchPlanRequest{Note: &note})
	defer br.Body.Close()
	if br.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(br.Body)
		t.Fatalf("branch %d %s", br.StatusCode, slurp)
	}
	var branched gen.Plan
	if err := json.NewDecoder(br.Body).Decode(&branched); err != nil {
		t.Fatal(err)
	}
	if branched.ParentPlanId == nil || string(*branched.ParentPlanId) != pid.String() {
		t.Fatalf("parent %+v", branched.ParentPlanId)
	}
	if branched.Id == changed.Id {
		t.Fatal("branch reused the original id")
	}
}

func TestEmptyInboxAndMemoryAreListsNot501(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	for _, path := range []string{"/approvals", "/memory", "/memory/proposals", "/checkpoints"} {
		res, err := http.Get(hs.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s %d %s", path, res.StatusCode, body)
		}
		if !strings.Contains(string(body), `"items"`) {
			t.Fatalf("%s body %s", path, body)
		}
	}
}
