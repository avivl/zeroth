package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

func mustGrantID(t testing.TB, raw string) store.GrantID {
	t.Helper()
	id, err := store.ParseGrantID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustLeaseID(t testing.TB, raw string) store.LeaseID {
	t.Helper()
	id, err := store.ParseLeaseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fullAgent(t *testing.T, id string, created time.Time) store.Agent {
	t.Helper()
	return store.Agent{
		ID:             mustAID(t, id),
		Name:           "agent " + id,
		Harness:        "claudecode",
		Status:         "ready",
		Model:          "opus",
		Tools:          []string{"Read", "Edit"},
		AutonomyTier:   "supervised",
		ReviewerModel:  "sonnet",
		ReviewerModel2: "haiku",
		ReviewerDual:   true,
		BlockOnFail:    true,
		CreatedAt:      created,
		UpdatedAt:      created,
	}
}

func TestAgentRoundTripAndUpdate(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	want := fullAgent(t, "a1", created)
	if err := s.CreateAgent(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgent(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Name != want.Name || got.Harness != want.Harness || got.Status != want.Status {
		t.Fatalf("identity %+v", got)
	}
	if got.Model != want.Model || got.AutonomyTier != want.AutonomyTier {
		t.Fatalf("config %+v", got)
	}
	if got.ReviewerModel != want.ReviewerModel || got.ReviewerModel2 != want.ReviewerModel2 {
		t.Fatalf("reviewer %+v", got)
	}
	if !got.ReviewerDual || !got.BlockOnFail {
		t.Fatalf("reviewer flags %+v", got)
	}
	if len(got.Tools) != 2 || got.Tools[0] != "Read" || got.Tools[1] != "Edit" {
		t.Fatalf("tools %+v", got.Tools)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("created_at %s want %s", got.CreatedAt, created)
	}

	want.Status = "paused"
	want.Tools = nil
	want.ReviewerDual = false
	want.BlockOnFail = false
	want.UpdatedAt = created.Add(time.Minute)
	if err := s.UpdateAgent(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetAgent(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paused" || len(got.Tools) != 0 || got.ReviewerDual || got.BlockOnFail {
		t.Fatalf("after update %+v", got)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("updated_at %s want %s", got.UpdatedAt, want.UpdatedAt)
	}
}

func TestAgentValidationAndMissingRows(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	ok := fullAgent(t, "a1", created)

	cases := []struct {
		name string
		mut  func(a *store.Agent)
	}{
		{"zero id", func(a *store.Agent) { a.ID = store.AgentID{} }},
		{"empty name", func(a *store.Agent) { a.Name = "" }},
		{"empty harness", func(a *store.Agent) { a.Harness = "" }},
		{"empty status", func(a *store.Agent) { a.Status = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := ok
			tc.mut(&bad)
			if err := s.CreateAgent(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("create err = %v, want ErrInvalid", err)
			}
			if err := s.UpdateAgent(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("update err = %v, want ErrInvalid", err)
			}
		})
	}

	if _, err := s.GetAgent(ctx, store.AgentID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("get zero id err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetAgent(ctx, mustAID(t, "a_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get absent err = %v, want ErrNotFound", err)
	}
	if err := s.UpdateAgent(ctx, fullAgent(t, "a_absent", created)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update absent err = %v, want ErrNotFound", err)
	}
}

func TestListAgentsPages(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	for i, id := range []string{"a1", "a2", "a3"} {
		a := fullAgent(t, id, base.Add(time.Duration(i)*time.Minute))
		if err := s.CreateAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.ListAgents(ctx, store.PageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 3 {
		t.Fatalf("listed %d, want 3", len(all.Items))
	}

	first, err := s.ListAgents(ctx, store.PageQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Next == "" {
		t.Fatalf("page 1 = %d items, next %q", len(first.Items), first.Next)
	}
	second, err := s.ListAgents(ctx, store.PageQuery{Limit: 2, Cursor: first.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Next != "" {
		t.Fatalf("page 2 = %d items, next %q", len(second.Items), second.Next)
	}
	if _, err := s.ListAgents(ctx, store.PageQuery{Cursor: "not-a-cursor"}); err == nil {
		t.Fatal("bad cursor accepted")
	}
}

func TestLeaseLifecycle(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	agent := fullAgent(t, "a1", created)
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}

	want := store.Lease{
		ID:        mustLeaseID(t, "l1"),
		GrantID:   mustGrantID(t, "g1"),
		ScopeID:   mustScopeID(t, "scope-a"),
		AgentID:   agent.ID,
		ExpiresAt: created.Add(time.Hour),
		MintedAt:  created,
	}
	if err := s.CreateLease(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetLease(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.GrantID != want.GrantID || got.ScopeID != want.ScopeID || got.AgentID != want.AgentID {
		t.Fatalf("identity %+v", got)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) || !got.MintedAt.Equal(want.MintedAt) {
		t.Fatalf("times %+v", got)
	}

	want.ExpiresAt = created.Add(2 * time.Hour)
	if err := s.UpdateLease(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, err = s.GetLease(ctx, want.ID); err != nil || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("after update %+v err=%v", got, err)
	}

	byAgent, err := s.ListLeases(ctx, store.LeaseQuery{AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAgent.Items) != 1 {
		t.Fatalf("agent filter listed %d, want 1", len(byAgent.Items))
	}
	none, err := s.ListLeases(ctx, store.LeaseQuery{AgentID: mustAID(t, "a_absent")})
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Items) != 0 {
		t.Fatalf("unknown agent listed %d", len(none.Items))
	}

	if err := s.DeleteLease(ctx, want.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLease(ctx, want.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteLease(ctx, want.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteLease(ctx, store.LeaseID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("delete zero id err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetLease(ctx, store.LeaseID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("get zero id err = %v, want ErrInvalid", err)
	}
	if err := s.UpdateLease(ctx, want); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update deleted err = %v, want ErrNotFound", err)
	}
}

func TestLeaseWriteValidation(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	ok := store.Lease{
		ID: mustLeaseID(t, "l1"), GrantID: mustGrantID(t, "g1"),
		ScopeID: mustScopeID(t, "scope-a"), AgentID: mustAID(t, "a1"),
		ExpiresAt: created.Add(time.Hour), MintedAt: created,
	}
	cases := []struct {
		name string
		mut  func(l *store.Lease)
	}{
		{"zero id", func(l *store.Lease) { l.ID = store.LeaseID{} }},
		{"zero grant", func(l *store.Lease) { l.GrantID = store.GrantID{} }},
		{"zero scope", func(l *store.Lease) { l.ScopeID = store.ScopeID{} }},
		{"zero agent", func(l *store.Lease) { l.AgentID = store.AgentID{} }},
		{"zero expiry", func(l *store.Lease) { l.ExpiresAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := ok
			tc.mut(&bad)
			if err := s.CreateLease(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("create err = %v, want ErrInvalid", err)
			}
			if err := s.UpdateLease(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("update err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCrossExamStatsCountsVerdictsAndSilentPasses(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	agentID := mustAID(t, "a1")
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	// A reviewer that passes a nontrivial plan with no notes is the silent
	// pass the scoreboard exists to surface.
	long := make([]byte, 100)
	for i := range long {
		long[i] = 'x'
	}
	seed := []struct {
		id, verdict, reasoning string
		effects                []store.PlanEffect
	}{
		{"p1", "pass", "in scope", []store.PlanEffect{{Type: "modify", Path: "a", Diff: "small"}}},
		{"p2", "fail", "out of scope", []store.PlanEffect{{Type: "modify", Path: "b", Diff: "small"}}},
		{"p3", "pass_with_notes", "narrow it", []store.PlanEffect{{Type: "modify", Path: "c", Diff: "small"}}},
		{"p4", "pass", "", []store.PlanEffect{{Type: "modify", Path: "d", Diff: string(long)}}},
		{"p5", "pass", "", []store.PlanEffect{{Type: "modify", Path: "e", Diff: "small"}}},
	}
	for i, sd := range seed {
		p := fullPlan(t, sid, sd.id, base.Add(time.Duration(i)*time.Minute))
		p.Effects = sd.effects
		p.CrossExam = &store.CrossExam{Verdict: sd.verdict, ReviewerModel: "sonnet", Reasoning: sd.reasoning, At: base}
		if err := s.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	// An unexamined plan must not count toward Examined.
	plain := fullPlan(t, sid, "p6", base.Add(9*time.Minute))
	plain.CrossExam = nil
	if err := s.CreatePlan(ctx, plain); err != nil {
		t.Fatal(err)
	}

	stats, err := s.CrossExamStats(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.AgentID != agentID || stats.Examined != 5 {
		t.Fatalf("examined = %d, want 5: %+v", stats.Examined, stats)
	}
	if stats.Pass != 3 || stats.Fail != 1 || stats.PassWithNotes != 1 {
		t.Fatalf("verdicts %+v", stats)
	}
	// p4 only: p5 has empty notes but a trivial one-effect diff.
	if stats.EmptyNotesNontrivial != 1 {
		t.Fatalf("silent passes = %d, want 1: %+v", stats.EmptyNotesNontrivial, stats)
	}
	if got := stats.PassRate(); got != 0.8 {
		t.Fatalf("pass rate = %v, want 0.8", got)
	}

	if _, err := s.CrossExamStats(ctx, store.AgentID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("zero agent err = %v, want ErrInvalid", err)
	}
	if _, err := s.CrossExamStats(ctx, mustAID(t, "a_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("absent agent err = %v, want ErrNotFound", err)
	}
}
