package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/tracker"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func TestRetractClosesPRAndRecordsOnTracker(t *testing.T) {
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
	applied.Body.Close()
	if applied.StatusCode != http.StatusOK {
		t.Fatalf("apply %d", applied.StatusCode)
	}

	got := getRun(t, e.hs.URL, string(run.Id))
	if got.PullRequest == nil || *got.PullRequest != "https://github.com/avivl/zeroth/pull/99" {
		t.Fatalf("run pull_request %+v", got.PullRequest)
	}

	reason := "Apply overwrote README.md instead of patching it."
	retract := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: reason})
	defer retract.Body.Close()
	if retract.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(retract.Body)
		t.Fatalf("retract %d %s", retract.StatusCode, slurp)
	}
	var out gen.Run
	if err := json.NewDecoder(retract.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Status != gen.RunStatusRetracted {
		t.Fatalf("status %s", out.Status)
	}
	if out.RetractReason == nil || *out.RetractReason != reason {
		t.Fatalf("reason %+v", out.RetractReason)
	}
	if out.RetractedAt == nil {
		t.Fatal("missing retracted_at")
	}

	e.pub.mu.Lock()
	if len(e.pub.closed) != 1 || e.pub.closed[0] != "https://github.com/avivl/zeroth/pull/99" {
		t.Fatalf("closed PRs %v", e.pub.closed)
	}
	if !strings.Contains(e.pub.closeComment, reason) || !strings.Contains(e.pub.closeComment, string(run.Id)) {
		t.Fatalf("pr close comment %q", e.pub.closeComment)
	}
	e.pub.mu.Unlock()

	found := false
	for _, c := range e.tr.commentBodies() {
		if strings.Contains(c, "### Zeroth retracted") && strings.Contains(c, reason) && strings.Contains(c, "https://github.com/avivl/zeroth/pull/99") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing retract comment: %v", e.tr.commentBodies())
	}
	if e.tr.lastState() != tracker.StateUnstarted {
		t.Fatalf("issue state %q, want unstarted", e.tr.lastState())
	}
	if keys := e.tr.unassignKeys(); len(keys) != 1 || keys[0] != "42-50" {
		t.Fatalf("unassign %v", keys)
	}
	for _, c := range e.tr.commentBodies() {
		if strings.Contains(c, "### Zeroth cancelled") {
			t.Fatalf("retract posted a cancel comment: %s", c)
		}
	}

	e.tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-50", Issue: e.tr.issue, At: time.Now()}
	next := waitNewRunByTracker(t, e.hs.URL, "42-50", string(out.Id))
	if next.Id == out.Id {
		t.Fatal("re-assign reused the retracted run")
	}

	again := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: reason})
	defer again.Body.Close()
	if again.StatusCode != http.StatusConflict {
		slurp, _ := io.ReadAll(again.Body)
		t.Fatalf("second retract %d %s", again.StatusCode, slurp)
	}
}

func TestRetractLiveRunConflicts(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	run := createRun(t, e.hs.URL, "still working")
	res := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: "too soon"})
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("retract live %d %s", res.StatusCode, slurp)
	}
}

func TestRetractRequiresReason(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	run := createRun(t, e.hs.URL, "needs a reason")
	res, err := http.Post(e.hs.URL+"/runs/"+string(run.Id)+"/retract", "application/json", bytes.NewReader([]byte(`{"reason":""}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("empty reason %d %s", res.StatusCode, slurp)
	}
}

func TestRetractMergedPRConflicts(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)
	e.pub.mu.Lock()
	e.pub.closeErr = errors.New("pull request is already merged")
	e.pub.mu.Unlock()
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
	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	applied.Body.Close()
	if applied.StatusCode != http.StatusOK {
		t.Fatalf("apply %d", applied.StatusCode)
	}
	retract := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: "bad patch"})
	defer retract.Body.Close()
	if retract.StatusCode != http.StatusConflict {
		slurp, _ := io.ReadAll(retract.Body)
		t.Fatalf("merged retract %d %s", retract.StatusCode, slurp)
	}
}

func TestRetractRecordsWithoutPullRequest(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{Key: "42-56", Title: "bad output, no PR"}
	tr := newStubTracker(iss)
	sbx := newFakeSandbox()
	hs := assignServer(t, tr, sbx, 5*time.Millisecond, 2)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-56", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-56")
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)

	reason := "Harness failed after proposing a destructive plan."
	res := postJSON(t, hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: reason})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("retract %d %s", res.StatusCode, slurp)
	}
	found := false
	for _, c := range tr.commentBodies() {
		if strings.Contains(c, "### Zeroth retracted") && strings.Contains(c, reason) && strings.Contains(c, "none opened") {
			found = true
		}
		if strings.Contains(c, "### Zeroth cancelled") {
			t.Fatalf("retract posted a cancel comment: %s", c)
		}
	}
	if !found {
		t.Fatalf("missing retract comment: %v", tr.commentBodies())
	}
	if keys := tr.unassignKeys(); len(keys) != 1 || keys[0] != "42-56" {
		t.Fatalf("unassign %v", keys)
	}
}

func TestRetractRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	res, err := http.Post(hs.URL+"/runs/s_missing/retract", "application/json", bytes.NewReader([]byte(`{`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("invalid json %d %s", res.StatusCode, slurp)
	}
}

func TestRetractUnknownRun(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	res := postJSON(t, hs.URL+"/runs/s_missing/retract", gen.RetractRequest{Reason: "no such run"})
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusNotFound {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("missing run %d %s", res.StatusCode, slurp)
	}
}

func TestRetractCompletedRunWithoutPR(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	run := createRun(t, hs.URL, "no pull request")
	waitRunTerminal(t, hs.URL, string(run.Id))
	reason := "operator spotted a bad result before a PR existed"
	res := postJSON(t, hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: reason})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("retract %d %s", res.StatusCode, slurp)
	}
	var out gen.Run
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Status != gen.RunStatusRetracted {
		t.Fatalf("status %s", out.Status)
	}
	if out.RetractReason == nil || *out.RetractReason != reason {
		t.Fatalf("reason %+v", out.RetractReason)
	}
}

func TestRetractCommentFailure(t *testing.T) {
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
	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	applied.Body.Close()
	if applied.StatusCode != http.StatusOK {
		t.Fatalf("apply %d", applied.StatusCode)
	}
	e.tr.mu.Lock()
	e.tr.commentErr = errors.New("linear down")
	e.tr.mu.Unlock()
	retract := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: "cannot record"})
	defer retract.Body.Close()
	if retract.StatusCode == http.StatusOK {
		t.Fatal("expected comment failure")
	}
}

func TestRetractUnassignFailure(t *testing.T) {
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
	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	applied.Body.Close()
	if applied.StatusCode != http.StatusOK {
		t.Fatalf("apply %d", applied.StatusCode)
	}
	e.tr.mu.Lock()
	e.tr.unassignErr = errors.New("unassign refused")
	e.tr.mu.Unlock()
	retract := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: "bad patch"})
	defer retract.Body.Close()
	if retract.StatusCode == http.StatusOK {
		t.Fatal("expected unassign failure")
	}
}

func TestRetractSetStateFailureStillSucceeds(t *testing.T) {
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
	applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
	applied.Body.Close()
	if applied.StatusCode != http.StatusOK {
		t.Fatalf("apply %d", applied.StatusCode)
	}
	e.tr.mu.Lock()
	e.tr.stateErr = errors.New("workflow missing")
	e.tr.mu.Unlock()
	retract := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/retract", gen.RetractRequest{Reason: "bad patch"})
	defer retract.Body.Close()
	if retract.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(retract.Body)
		t.Fatalf("retract %d %s", retract.StatusCode, slurp)
	}
}

func waitRunTerminal(t *testing.T, base, id string) gen.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last gen.Run
	for time.Now().Before(deadline) {
		last = getRun(t, base, id)
		switch last.Status {
		case gen.RunStatusCompleted, gen.RunStatusFailed:
			return last
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish, status %s", id, last.Status)
	return last
}
