package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/server"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// 42-77 asked for this guard: two unrelated runs proposing byte-identical
// content to the same target must each publish their own PR. The reported
// cause (a global idempotency-key collision) is not reachable, because
// applyWorld is built per apply and its seen-map starts empty, but the
// guarantee is worth pinning so a future persistent seen-store cannot
// reintroduce the silent no-op.
func TestIdenticalContentInTwoRunsPublishesTwice(t *testing.T) {
	t.Parallel()
	e := applySetup(t)
	e.patchReviewer(false)

	applyOnce := func(t *testing.T, label string) (string, []string) {
		t.Helper()
		run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
		pid := e.seedPatchedFilePlan(run, []plan.Proposed{
			{Type: "create", Path: "CHANGELOG.md", Diff: "# Changelog\n\nStub.\n"},
		})
		if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
			t.Fatalf("%s exam: %v", label, err)
		}
		approve := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{})
		defer approve.Body.Close()
		if approve.StatusCode != http.StatusOK {
			slurp, _ := io.ReadAll(approve.Body)
			t.Fatalf("%s approve %d: %s", label, approve.StatusCode, slurp)
		}
		res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
		defer res.Body.Close()
		slurp, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s apply %d: %s", label, res.StatusCode, slurp)
		}
		var out gen.ApplyPlanResponse
		if err := json.Unmarshal(slurp, &out); err != nil {
			t.Fatalf("%s decode: %v", label, err)
		}
		e.pub.mu.Lock()
		targets := append([]string(nil), e.pub.req.Targets...)
		e.pub.mu.Unlock()
		return string(out.Plan.Status), targets
	}

	firstStatus, firstTargets := applyOnce(t, "run 1")
	if firstStatus != string(gen.PlanStatusApplied) {
		t.Fatalf("run 1 status %s", firstStatus)
	}
	if len(firstTargets) != 1 || firstTargets[0] != "CHANGELOG.md" {
		t.Fatalf("run 1 published %v, want [CHANGELOG.md]", firstTargets)
	}

	// Clear the record so run 2 cannot pass on run 1's evidence.
	e.pub.mu.Lock()
	e.pub.req = server.ApplyPublish{}
	e.pub.ref = server.ApplyRef{}
	e.pub.mu.Unlock()

	secondStatus, secondTargets := applyOnce(t, "run 2")
	if secondStatus != string(gen.PlanStatusApplied) {
		t.Fatalf("run 2 status %s", secondStatus)
	}
	// The bug this guards: run 2 silently publishing nothing while still
	// reporting applied, so the tracker moves to "In Review" with no PR.
	if len(secondTargets) != 1 || secondTargets[0] != "CHANGELOG.md" {
		t.Fatalf("run 2 published %v, want its own [CHANGELOG.md]", secondTargets)
	}
}
