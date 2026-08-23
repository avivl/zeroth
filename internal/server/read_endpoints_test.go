package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// getInto issues a GET and decodes a 200 body, failing on any other status.
func getInto(t *testing.T, url string, out any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("GET %s = %d: %s", url, res.StatusCode, slurp)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
}

// getStatus issues a GET and returns the status code and body.
func getStatus(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	slurp, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(slurp)
}

func TestListPlansFiltersAndPages(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	e.patchReviewer(false)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	for range 3 {
		e.seedPlan(run, []plan.Proposed{
			{Type: "modify", Path: "docs/design/plan.md", Diff: "-a\n+b"},
		}, map[string]string{"docs/design/plan.md": "pre"})
	}

	var all gen.PlanList
	getInto(t, e.hs.URL+"/plans", &all)
	if len(all.Items) != 3 {
		t.Fatalf("listed %d plans, want 3", len(all.Items))
	}

	var byRun gen.PlanList
	getInto(t, e.hs.URL+"/plans?run_id="+string(run.Id), &byRun)
	if len(byRun.Items) != 3 {
		t.Fatalf("run filter listed %d, want 3", len(byRun.Items))
	}

	var byStatus gen.PlanList
	getInto(t, e.hs.URL+"/plans?status=draft", &byStatus)
	if len(byStatus.Items) != 3 {
		t.Fatalf("status filter listed %d, want 3", len(byStatus.Items))
	}
	var none gen.PlanList
	getInto(t, e.hs.URL+"/plans?status=applied", &none)
	if len(none.Items) != 0 {
		t.Fatalf("applied filter listed %d, want 0", len(none.Items))
	}

	var page gen.PlanList
	getInto(t, e.hs.URL+"/plans?limit=2", &page)
	if len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatalf("page 1 = %d items, cursor %v", len(page.Items), page.NextCursor)
	}
	var rest gen.PlanList
	getInto(t, e.hs.URL+"/plans?limit=2&cursor="+*page.NextCursor, &rest)
	if len(rest.Items) != 1 || rest.NextCursor != nil {
		t.Fatalf("page 2 = %d items, cursor %v", len(rest.Items), rest.NextCursor)
	}

	if code, body := getStatus(t, e.hs.URL+"/plans?cursor=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("bad cursor = %d, want 400: %s", code, body)
	}
}

func TestGetPlanNotFound(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	if code, body := getStatus(t, e.hs.URL+"/plans/p_absent"); code != http.StatusNotFound {
		t.Fatalf("absent plan = %d, want 404: %s", code, body)
	}
}

func TestListRunsFiltersAndPages(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	for range 3 {
		createRun(t, e.hs.URL, "list runs")
	}

	var all gen.RunList
	getInto(t, e.hs.URL+"/runs", &all)
	if len(all.Items) != 3 {
		t.Fatalf("listed %d runs, want 3", len(all.Items))
	}

	var byAgent gen.RunList
	getInto(t, e.hs.URL+"/runs?agent_id="+server.DefaultAgentID, &byAgent)
	if len(byAgent.Items) != 3 {
		t.Fatalf("agent filter listed %d, want 3", len(byAgent.Items))
	}
	var byStatus gen.RunList
	getInto(t, e.hs.URL+"/runs?status=completed", &byStatus)
	if len(byStatus.Items) != 0 {
		t.Fatalf("completed filter listed %d, want 0", len(byStatus.Items))
	}

	var page gen.RunList
	getInto(t, e.hs.URL+"/runs?limit=2", &page)
	if len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatalf("page 1 = %d items, cursor %v", len(page.Items), page.NextCursor)
	}
	var rest gen.RunList
	getInto(t, e.hs.URL+"/runs?limit=2&cursor="+*page.NextCursor, &rest)
	if len(rest.Items) != 1 {
		t.Fatalf("page 2 = %d items", len(rest.Items))
	}

	if code, _ := getStatus(t, e.hs.URL+"/runs?cursor=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("bad cursor = %d, want 400", code)
	}
	if code, _ := getStatus(t, e.hs.URL+"/runs/s_absent"); code != http.StatusNotFound {
		t.Fatalf("absent run = %d, want 404", code)
	}
}

func TestStopRunIsNotImplementedInThisMilestone(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	run := createRun(t, e.hs.URL, "stop me")

	res := postJSON(t, e.hs.URL+"/runs/"+string(run.Id)+"/stop", struct{}{})
	defer res.Body.Close()
	slurp, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("stop = %d, want 501: %s", res.StatusCode, slurp)
	}
}

func TestAgentReadEndpoints(t *testing.T) {
	t.Parallel()
	e := examSetup(t)

	var list gen.AgentList
	getInto(t, e.hs.URL+"/agents", &list)
	if len(list.Items) == 0 {
		t.Fatal("no agents listed; the default agent should exist")
	}

	var one gen.Agent
	getInto(t, e.hs.URL+"/agents/"+server.DefaultAgentID, &one)
	if string(one.Id) != server.DefaultAgentID {
		t.Fatalf("agent id %q", one.Id)
	}

	// An agent that has examined nothing reports a zero scoreboard rather
	// than a 404, so the UI can render it before the first run.
	var stats gen.CrossExamStats
	getInto(t, e.hs.URL+"/agents/"+server.DefaultAgentID+"/cross-exam-stats", &stats)
	if stats.Examined != 0 || stats.PassRate != 0 {
		t.Fatalf("fresh agent stats %+v", stats)
	}

	var leases gen.LeaseList
	getInto(t, e.hs.URL+"/agents/"+server.DefaultAgentID+"/leases", &leases)
	if leases.Items == nil {
		t.Fatal("leases items is null, want an empty list")
	}

	for _, path := range []string{
		"/agents/a_absent",
		"/agents/a_absent/cross-exam-stats",
		"/agents/a_absent/leases",
	} {
		if code, body := getStatus(t, e.hs.URL+path); code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404: %s", path, code, body)
		}
	}

	if code, _ := getStatus(t, e.hs.URL+"/agents?cursor=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("bad cursor = %d, want 400", code)
	}
}

func TestPatchAgentRejectsBadBody(t *testing.T) {
	t.Parallel()
	e := examSetup(t)

	req, err := http.NewRequest(http.MethodPatch, e.hs.URL+"/agents/"+server.DefaultAgentID, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("empty patch = %d, want 400: %s", res.StatusCode, slurp)
	}
}

func TestAuditReadEndpoints(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	// Creating a run writes run.create into the chain, so there is at least
	// one record to list and verify.
	createRun(t, e.hs.URL, "audit read")

	var list gen.AuditList
	getInto(t, e.hs.URL+"/audit", &list)
	if len(list.Items) == 0 {
		t.Fatal("audit list is empty; creating a run should have appended")
	}

	var byType gen.AuditList
	getInto(t, e.hs.URL+"/audit?resource_type=run", &byType)
	if len(byType.Items) == 0 {
		t.Fatal("resource_type filter dropped every run record")
	}
	var byID gen.AuditList
	getInto(t, e.hs.URL+"/audit?resource_id="+byType.Items[0].ResourceId, &byID)
	if len(byID.Items) == 0 {
		t.Fatal("resource_id filter dropped its own record")
	}

	// Verify recomputes the signature over the stored payload, so a record
	// straight out of the chain has to come back valid.
	vres := postJSON(t, e.hs.URL+"/audit/"+string(list.Items[0].Id)+"/verify", struct{}{})
	defer vres.Body.Close()
	if vres.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(vres.Body)
		t.Fatalf("verify = %d, want 200: %s", vres.StatusCode, slurp)
	}
	var verdict gen.AuditVerification
	if err := json.NewDecoder(vres.Body).Decode(&verdict); err != nil {
		t.Fatal(err)
	}
	if !verdict.Valid {
		t.Fatalf("freshly written record failed verification: %+v", verdict)
	}

	absent := postJSON(t, e.hs.URL+"/audit/aud_absent/verify", struct{}{})
	defer absent.Body.Close()
	if absent.StatusCode != http.StatusNotFound {
		slurp, _ := io.ReadAll(absent.Body)
		t.Fatalf("absent record verify = %d, want 404: %s", absent.StatusCode, slurp)
	}
	if code, _ := getStatus(t, e.hs.URL+"/audit?cursor=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("bad cursor = %d, want 400", code)
	}
}

func TestMemoryReadAndCreate(t *testing.T) {
	t.Parallel()
	e := examSetup(t)

	body := gen.CreateMemoryRequest{
		Kind:    gen.MemoryKindOperator,
		Content: "deploys land Tuesdays",
	}
	res := postJSON(t, e.hs.URL+"/memory", body)
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("create memory = %d, want 201: %s", res.StatusCode, slurp)
	}
	var created gen.MemoryEntry
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Content != body.Content {
		t.Fatalf("created %+v", created)
	}

	var list gen.MemoryList
	getInto(t, e.hs.URL+"/memory", &list)
	if len(list.Items) != 1 {
		t.Fatalf("listed %d memory entries, want 1", len(list.Items))
	}
	var byKind gen.MemoryList
	getInto(t, e.hs.URL+"/memory?kind=operator", &byKind)
	if len(byKind.Items) != 1 {
		t.Fatalf("kind filter listed %d, want 1", len(byKind.Items))
	}
	var otherKind gen.MemoryList
	getInto(t, e.hs.URL+"/memory?kind=session", &otherKind)
	if len(otherKind.Items) != 0 {
		t.Fatalf("session filter listed %d, want 0", len(otherKind.Items))
	}

	var proposals gen.MemoryProposalList
	getInto(t, e.hs.URL+"/memory/proposals", &proposals)
	if proposals.Items == nil {
		t.Fatal("proposals items is null, want an empty list")
	}
	getInto(t, e.hs.URL+"/memory/proposals?status=pending", &proposals)

	empty := postJSON(t, e.hs.URL+"/memory", gen.CreateMemoryRequest{Kind: gen.MemoryKindOperator})
	defer empty.Body.Close()
	if empty.StatusCode != http.StatusBadRequest {
		slurp, _ := io.ReadAll(empty.Body)
		t.Fatalf("empty content = %d, want 400: %s", empty.StatusCode, slurp)
	}

	if code, _ := getStatus(t, e.hs.URL+"/memory?cursor=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("bad cursor = %d, want 400", code)
	}
	if code, _ := getStatus(t, e.hs.URL+"/memory/proposals?cursor=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("bad proposals cursor = %d, want 400", code)
	}
}

func TestCheckpointAndApprovalListsAreEmptyNotNull(t *testing.T) {
	t.Parallel()
	e := examSetup(t)

	// An empty collection is a list, not null: the UI iterates these.
	var cks gen.CheckpointList
	getInto(t, e.hs.URL+"/checkpoints", &cks)
	if cks.Items == nil {
		t.Fatal("checkpoints items is null")
	}
	var apps gen.ApprovalList
	getInto(t, e.hs.URL+"/approvals", &apps)
	if apps.Items == nil {
		t.Fatal("approvals items is null")
	}
	getInto(t, e.hs.URL+"/approvals?status=pending", &apps)

	if code, _ := getStatus(t, e.hs.URL+"/checkpoints?cursor=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("bad checkpoint cursor = %d, want 400", code)
	}
	if code, _ := getStatus(t, e.hs.URL+"/approvals?cursor=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("bad approval cursor = %d, want 400", code)
	}
	if code, _ := getStatus(t, e.hs.URL+"/checkpoints?run_id=s_absent"); code != http.StatusOK {
		t.Fatalf("unknown run checkpoints = %d, want 200 with an empty list", code)
	}
}

func TestRestoreCheckpointRejectsUnknownID(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	res := postJSON(t, e.hs.URL+"/checkpoints/ck_absent/restore", struct{}{})
	defer res.Body.Close()
	slurp, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("restore absent = %d, want 404: %s", res.StatusCode, slurp)
	}
}

func TestHealthReportsReady(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	var body map[string]any
	getInto(t, e.hs.URL+"/health", &body)
	if len(body) == 0 {
		t.Fatal("health returned an empty object")
	}
}

func TestSteerAndMutateRejectUnknownRun(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	msg := "narrow the diff"
	for _, tc := range []struct {
		path string
		body any
	}{
		{"/runs/s_absent/steer", gen.SteerRequest{Message: msg}},
		{"/runs/s_absent/background", struct{}{}},
		{"/runs/s_absent/foreground", struct{}{}},
		{"/runs/s_absent/retract", gen.RetractRequest{Reason: "wrong tree"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			res := postJSON(t, e.hs.URL+tc.path, tc.body)
			defer res.Body.Close()
			slurp, _ := io.ReadAll(res.Body)
			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("%s = %d, want 404: %s", tc.path, res.StatusCode, slurp)
			}
		})
	}
}

func TestRunEventsReplayAfterCursor(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	run := createRun(t, e.hs.URL, "events replay")
	sid, err := store.ParseSessionID(string(run.Id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AppendEvents(t.Context(), sid, []store.Event{
		{Type: "token", Message: "one", CreatedAt: time.Now().UTC()},
		{Type: "token", Message: "two", CreatedAt: time.Now().UTC()},
	}); err != nil {
		t.Fatal(err)
	}

	var page gen.RunEventList
	getInto(t, e.hs.URL+"/runs/"+string(run.Id)+"/events", &page)
	if len(page.Items) < 2 {
		t.Fatalf("listed %d events, want the appended pair", len(page.Items))
	}
	if code, _ := getStatus(t, e.hs.URL+"/runs/s_absent/events"); code != http.StatusNotFound {
		t.Fatalf("absent run events = %d, want 404", code)
	}
}
