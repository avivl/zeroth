package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/server"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func TestGoldenApproveAndApplyOverHTTP(t *testing.T) {
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
	// The run is terminal after apply, so an on-demand checkpoint may 409.
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
	if len(list.Items) == 0 {
		t.Fatal("expected apply-minted leases")
	}
	if time.Now().After(list.Items[0].ExpiresAt.Add(time.Hour)) {
		t.Fatalf("lease expiry %+v", list.Items[0].ExpiresAt)
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
