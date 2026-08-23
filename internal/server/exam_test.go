package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

const producerCoT = "PRODUCER_CHAIN_OF_THOUGHT_do_not_leak_this_token"

type capturingReviewer struct {
	last string
}

func (c *capturingReviewer) Review(_ context.Context, model string, packet plan.Packet) (plan.Review, error) {
	c.last = packet.Encode()
	if strings.Contains(c.last, producerCoT) {
		return plan.Review{}, context.Canceled
	}
	if strings.Contains(c.last, ".ssh/") || strings.Contains(c.last, "secrets.env") {
		return plan.Review{Verdict: plan.VerdictFail, Notes: "scope violation in diffs", Model: model}, nil
	}
	return plan.Review{Verdict: plan.VerdictPass, Notes: "in scope", Model: model}, nil
}

// failingReviewer stands in for a reviewer outage, which is what 42-53 will
// make reachable once a real independent reviewer is wired up.
type failingReviewer struct{}

func (failingReviewer) Review(context.Context, string, plan.Packet) (plan.Review, error) {
	return plan.Review{}, errors.New("reviewer unavailable")
}

type examEnv struct {
	t   *testing.T
	st  store.Store
	srv *server.Server
	hs  *httptest.Server
	rev *capturingReviewer
}

func examSetup(t *testing.T) *examEnv {
	t.Helper()
	rev := &capturingReviewer{}
	e := examSetupWith(t, rev)
	e.rev = rev
	return e
}

func examSetupWith(t *testing.T, reviewer plan.Reviewer) *examEnv {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		Reviewer:      reviewer,
		TokenInterval: time.Hour,
		TokenCount:    1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return &examEnv{t: t, st: st, srv: srv, hs: hs}
}

func (e *examEnv) patchReviewer(block bool) {
	e.t.Helper()
	model := "sonnet"
	body, _ := json.Marshal(gen.AgentPatch{Reviewer: &gen.ReviewerConfig{
		Model:       &model,
		BlockOnFail: &block,
	}})
	req, err := http.NewRequest(http.MethodPatch, e.hs.URL+"/agents/"+server.DefaultAgentID, bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		e.t.Fatalf("patch %d %s", res.StatusCode, slurp)
	}
}

func (e *examEnv) seedPlan(run gen.Run, effects []plan.Proposed, observed map[string]string) store.PlanID {
	return e.seedPlanWithBodies(run, effects, observed, nil)
}

func (e *examEnv) seedPlanWithBodies(run gen.Run, effects []plan.Proposed, observed, bodies map[string]string) store.PlanID {
	e.t.Helper()
	pid, err := plan.NewID()
	if err != nil {
		e.t.Fatal(err)
	}
	sessID, err := session.ParseID(string(run.Id))
	if err != nil {
		e.t.Fatal(err)
	}
	built, err := plan.Build(plan.Draft{
		ID:        pid,
		SessionID: sessID,
		Summary:   "draft for cross-exam",
		Effects:   effects,
		Observed:  observed,
		Bodies:    bodies,
		Lease:     "lease-1",
		ExpiresAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Scope:     "scope-a",
		Now:       time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		e.t.Fatal(err)
	}
	rec, err := built.Record()
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.st.CreatePlan(e.t.Context(), rec); err != nil {
		e.t.Fatal(err)
	}
	return rec.ID
}

func TestExamineDraftCatchesScopeViolationAndHidesProducerCoT(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	e.patchReviewer(false)
	prompt := "Fix the docs typo.\n\nAllowed-paths: docs/"
	run := createRun(t, e.hs.URL, prompt)
	sid, err := store.ParseSessionID(string(run.Id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AppendEvent(t.Context(), sid, store.Event{
		Type:    "token",
		Message: producerCoT + " I will rewrite .ssh/config",
		Payload: producerCoT,
	}); err != nil {
		t.Fatal(err)
	}
	logged, err := e.st.ReplayLast(t.Context(), sid, 20)
	if err != nil {
		t.Fatal(err)
	}
	var sawCoT bool
	for _, ev := range logged {
		if strings.Contains(ev.Message, producerCoT) || strings.Contains(ev.Payload, producerCoT) {
			sawCoT = true
			break
		}
	}
	if !sawCoT {
		t.Fatal("producer chain of thought was not in the session log")
	}
	pid := e.seedPlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
		{Type: "create", Path: ".ssh/authorized_keys", Diff: "ssh-ed25519 AAAA sneak"},
	}, map[string]string{"docs/design/plan.md": "pre"})

	out, err := e.srv.ExamineDraft(t.Context(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if out.Exam.Verdict != plan.VerdictFail {
		t.Fatalf("verdict %s", out.Exam.Verdict)
	}
	if strings.Contains(e.rev.last, producerCoT) {
		t.Fatal("reviewer saw producer chain of thought")
	}
	if !strings.Contains(e.rev.last, ".ssh/authorized_keys") {
		t.Fatal("reviewer packet missing the violating diff")
	}
	if strings.Contains(e.rev.last, "token-") {
		t.Fatal("reviewer packet included producer stream tokens")
	}

	res, err := http.Get(e.hs.URL + "/plans/" + pid.String())
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got gen.Plan
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.CrossExam == nil || got.CrossExam.Verdict != "fail" {
		t.Fatalf("plan cross_exam %+v", got.CrossExam)
	}
	if got.CrossExam.Reasoning == "" {
		t.Fatal("notes not on the plan")
	}
}

func TestExamineDraftBlockOnFailSteersAgent(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	e.patchReviewer(true)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	pid := e.seedPlan(run, []plan.Proposed{
		{Type: "create", Path: "secrets.env", Diff: "TOKEN=aaaa"},
	}, nil)

	out, err := e.srv.ExamineDraft(t.Context(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Returned {
		t.Fatal("expected return to agent")
	}
	got := getRun(t, e.hs.URL, string(run.Id))
	if got.Status != gen.RunStatusRunning {
		t.Fatalf("run status %s, want running", got.Status)
	}
	events := replayEvents(t, e.hs.URL, string(run.Id), 50)
	var sawVerdict, sawSteer bool
	for _, ev := range events {
		if ev.Type == "cross_exam_verdict" {
			sawVerdict = true
			if ev.Message == nil || !strings.Contains(*ev.Message, "fail") {
				t.Fatalf("verdict event %+v", ev)
			}
		}
		if ev.Type == "log" && ev.Message != nil && strings.Contains(*ev.Message, "cross-exam failed") {
			sawSteer = true
		}
	}
	if !sawVerdict || !sawSteer {
		t.Fatalf("events verdict=%v steer=%v items=%d", sawVerdict, sawSteer, len(events))
	}

	res, err := http.Get(e.hs.URL + "/plans/" + pid.String())
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var p gen.Plan
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Status != gen.PlanStatusChangesRequested {
		t.Fatalf("plan status %s", p.Status)
	}
	if p.ReviewComment == nil || *p.ReviewComment == "" {
		t.Fatal("notes not attached to the plan")
	}
}

func TestCrossExamPassRateIsQueryable(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	e.patchReviewer(false)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	passID := e.seedPlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	}, map[string]string{"docs/design/plan.md": "pre"})
	if _, err := e.srv.ExamineDraft(t.Context(), passID); err != nil {
		t.Fatal(err)
	}

	res, err := http.Get(e.hs.URL + "/agents/" + server.DefaultAgentID + "/cross-exam-stats")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("stats %d %s", res.StatusCode, slurp)
	}
	var stats gen.CrossExamStats
	if err := json.NewDecoder(res.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats.Examined != 1 || stats.Pass != 1 || stats.PassRate != 1 {
		t.Fatalf("stats %+v", stats)
	}
}

func TestChatReviewerExamineDraftSurfacesRealVerdict(t *testing.T) {
	t.Parallel()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		reply := "VERDICT: pass\nNOTES:\npaths match the issue"
		if strings.Contains(string(raw), ".ssh/") {
			reply = "VERDICT: fail\nNOTES:\nscope violation: .ssh/authorized_keys"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": reply}},
			},
		})
	}))
	t.Cleanup(hs.Close)
	rev, err := server.NewChatReviewer(server.ChatReviewerConfig{
		Model:      "gpt-4o",
		BaseURL:    hs.URL,
		APIKey:     "test-reviewer-key",
		HTTPClient: hs.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:                st,
		Reviewer:             rev,
		DefaultReviewerModel: "gpt-4o",
		TokenInterval:        time.Hour,
		TokenCount:           1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	api := httptest.NewServer(srv.Handler())
	t.Cleanup(api.Close)

	agentRes, err := http.Get(api.URL + "/agents/" + server.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	defer agentRes.Body.Close()
	var agent gen.Agent
	if err := json.NewDecoder(agentRes.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	if agent.Reviewer == nil || agent.Reviewer.Model == nil || *agent.Reviewer.Model != "gpt-4o" {
		t.Fatalf("default agent reviewer %+v", agent.Reviewer)
	}

	e := &examEnv{t: t, st: st, srv: srv, hs: api}
	run := createRun(t, api.URL, "Fix the docs typo.\n\nAllowed-paths: docs/")
	pid := e.seedPlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
		{Type: "create", Path: ".ssh/authorized_keys", Diff: "ssh-ed25519 AAAA sneak"},
	}, map[string]string{"docs/design/plan.md": "pre"})

	out, err := srv.ExamineDraft(t.Context(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if out.Exam.Verdict != plan.VerdictFail {
		t.Fatalf("verdict %s notes %q", out.Exam.Verdict, out.Exam.Reasoning)
	}
	if out.Exam.ReviewerModel != "gpt-4o" {
		t.Fatalf("reviewer model %q", out.Exam.ReviewerModel)
	}
	if !strings.Contains(out.Exam.Reasoning, ".ssh/authorized_keys") {
		t.Fatalf("notes %q", out.Exam.Reasoning)
	}
	if out.Exam.Reasoning == server.PassThroughNotes {
		t.Fatal("placeholder notes from a configured reviewer")
	}

	res, err := http.Get(api.URL + "/plans/" + pid.String())
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got gen.Plan
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.CrossExam == nil || got.CrossExam.Verdict != "fail" {
		t.Fatalf("plan cross_exam %+v", got.CrossExam)
	}

	inbox, err := http.Get(api.URL + "/approvals?status=pending")
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Body.Close()
	var approvals gen.ApprovalList
	if err := json.NewDecoder(inbox.Body).Decode(&approvals); err != nil {
		t.Fatal(err)
	}
	if len(approvals.Items) != 1 || approvals.Items[0].Summary == nil {
		t.Fatalf("inbox %+v", approvals.Items)
	}
	if !strings.HasPrefix(*approvals.Items[0].Summary, "fail:") {
		t.Fatalf("approval summary %q should lead with the verdict", *approvals.Items[0].Summary)
	}
}

func TestBranchPlanSurfacesExamFailure(t *testing.T) {
	t.Parallel()
	// 42-68: BranchPlan swallowed the ExamineDraft error and still returned
	// 201, so a branch whose cross-exam never ran read as a clean success.
	e := examSetupWith(t, failingReviewer{})
	e.patchReviewer(false)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	pid := e.seedPlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	}, map[string]string{"docs/design/plan.md": "pre"})

	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/branch", gen.BranchPlanRequest{})
	defer res.Body.Close()
	slurp, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("branch status = %d, want 500 when cross-exam fails: %s", res.StatusCode, slurp)
	}
	if !strings.Contains(string(slurp), "cross-exam") {
		t.Fatalf("error body = %s, want it to name cross-exam", slurp)
	}
}
