package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

func mustPID(t testing.TB, raw string) store.PlanID {
	t.Helper()
	id, err := store.ParsePlanID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustApID(t testing.TB, raw string) store.ApprovalID {
	t.Helper()
	id, err := store.ParseApprovalID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// planFixture seeds the agent and session a plan row points at, so foreign
// keys are satisfied without every test repeating the setup.
func planFixture(t *testing.T, s *Store) store.SessionID {
	t.Helper()
	ctx := t.Context()
	agent := store.Agent{ID: mustAID(t, "a1"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	sid := mustSID(t, "s1")
	now := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	sess := store.Session{ID: sid, AgentID: agent.ID, Status: "running", CreatedAt: now, UpdatedAt: now}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	return sid
}

func fullPlan(t *testing.T, sid store.SessionID, id string, created time.Time) store.Plan {
	t.Helper()
	return store.Plan{
		ID:           mustPID(t, id),
		SessionID:    sid,
		ParentPlanID: mustPID(t, "p_parent"),
		Status:       "draft",
		Summary:      "seeded plan",
		Hash:         "h_" + id,
		ExpiresAt:    created.Add(time.Hour),
		CostCeiling:  4200,
		ScopeID:      mustScopeID(t, "scope-a"),
		Credentials:  []store.CredentialConstraint{{Provider: "github", Kind: "token"}},
		Effects: []store.PlanEffect{{
			Type:              "modify",
			Path:              "README.md",
			Diff:              "-a\n+b",
			PreconditionHash:  "pre",
			PostconditionHash: "post",
			IdempotencyKey:    "idem-1",
			CostEstimate:      "0",
		}},
		CrossExam: &store.CrossExam{
			Verdict:       "pass",
			ReviewerModel: "sonnet",
			Reasoning:     "in scope",
			At:            created,
		},
		SecretScanFindings: []store.SecretScanFinding{{Path: ".env", Rule: "generic-api-key", Line: 3}},
		ReviewComment:      "looks fine",
		CreatedAt:          created,
		UpdatedAt:          created,
	}
}

func mustScopeID(t testing.TB, raw string) store.ScopeID {
	t.Helper()
	id, err := store.ParseScopeID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPlanRoundTripKeepsEveryColumn(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	want := fullPlan(t, sid, "p1", created)
	if err := s.CreatePlan(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetPlan(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.SessionID != want.SessionID || got.ParentPlanID != want.ParentPlanID {
		t.Fatalf("ids %+v", got)
	}
	if got.Status != want.Status || got.Summary != want.Summary || got.Hash != want.Hash {
		t.Fatalf("scalars %+v", got)
	}
	if got.CostCeiling != want.CostCeiling || got.ScopeID != want.ScopeID || got.ReviewComment != want.ReviewComment {
		t.Fatalf("scalars2 %+v", got)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) || !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times %+v", got)
	}
	if len(got.Effects) != 1 || got.Effects[0] != want.Effects[0] {
		t.Fatalf("effects %+v", got.Effects)
	}
	if len(got.Credentials) != 1 || got.Credentials[0] != want.Credentials[0] {
		t.Fatalf("credentials %+v", got.Credentials)
	}
	if len(got.SecretScanFindings) != 1 || got.SecretScanFindings[0] != want.SecretScanFindings[0] {
		t.Fatalf("findings %+v", got.SecretScanFindings)
	}
	if got.CrossExam == nil || got.CrossExam.Verdict != "pass" || got.CrossExam.ReviewerModel != "sonnet" {
		t.Fatalf("cross exam %+v", got.CrossExam)
	}
	if !got.CrossExam.At.Equal(want.CrossExam.At) {
		t.Fatalf("cross exam at %s want %s", got.CrossExam.At, want.CrossExam.At)
	}
}

func TestPlanWriteValidation(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	ok := fullPlan(t, sid, "p1", time.Unix(0, 1_700_000_000_000_000_000).UTC())

	cases := []struct {
		name  string
		mutot func(p *store.Plan)
	}{
		{"zero id", func(p *store.Plan) { p.ID = store.PlanID{} }},
		{"zero session", func(p *store.Plan) { p.SessionID = store.SessionID{} }},
		{"empty status", func(p *store.Plan) { p.Status = "" }},
		{"empty summary", func(p *store.Plan) { p.Summary = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := ok
			tc.mutot(&bad)
			if err := s.CreatePlan(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("create err = %v, want ErrInvalid", err)
			}
			if err := s.UpdatePlan(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("update err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestPlanMissingRows(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)

	if _, err := s.GetPlan(ctx, store.PlanID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("get zero id err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetPlan(ctx, mustPID(t, "p_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get absent err = %v, want ErrNotFound", err)
	}
	absent := fullPlan(t, sid, "p_absent", time.Unix(0, 1_700_000_000_000_000_000).UTC())
	if err := s.UpdatePlan(ctx, absent); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update absent err = %v, want ErrNotFound", err)
	}
}

func TestUpdatePlanOverwritesAndClearsCrossExam(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	p := fullPlan(t, sid, "p1", created)
	if err := s.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}

	p.Status = "approved"
	p.Summary = "revised"
	p.CrossExam = nil
	p.Effects = nil
	p.SecretScanFindings = nil
	p.Credentials = nil
	p.UpdatedAt = created.Add(time.Minute)
	if err := s.UpdatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "approved" || got.Summary != "revised" {
		t.Fatalf("update did not land: %+v", got)
	}
	if got.CrossExam != nil {
		t.Fatalf("cross exam survived a nil write: %+v", got.CrossExam)
	}
	if len(got.Effects) != 0 || len(got.SecretScanFindings) != 0 || len(got.Credentials) != 0 {
		t.Fatalf("slices survived a nil write: %+v", got)
	}
	if !got.UpdatedAt.Equal(p.UpdatedAt) {
		t.Fatalf("updated_at %s want %s", got.UpdatedAt, p.UpdatedAt)
	}
}

func TestListPlansFiltersOrdersAndPages(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	// Three plans on the seeded session, one on another, and one approved so
	// the status filter has something to exclude.
	for i, id := range []string{"p1", "p2", "p3"} {
		p := fullPlan(t, sid, id, base.Add(time.Duration(i)*time.Minute))
		if err := s.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	other := fullPlan(t, sid, "p4", base.Add(4*time.Minute))
	other.Status = "approved"
	if err := s.CreatePlan(ctx, other); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListPlans(ctx, store.PlanQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 4 {
		t.Fatalf("listed %d, want 4", len(all.Items))
	}
	if all.Items[0].ID.String() != "p4" || all.Items[3].ID.String() != "p1" {
		t.Fatalf("not newest first: %s..%s", all.Items[0].ID, all.Items[3].ID)
	}

	drafts, err := s.ListPlans(ctx, store.PlanQuery{Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts.Items) != 3 {
		t.Fatalf("status filter listed %d, want 3", len(drafts.Items))
	}

	bySession, err := s.ListPlans(ctx, store.PlanQuery{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession.Items) != 4 {
		t.Fatalf("session filter listed %d, want 4", len(bySession.Items))
	}
	none, err := s.ListPlans(ctx, store.PlanQuery{SessionID: mustSID(t, "s_absent")})
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Items) != 0 || none.Next != "" {
		t.Fatalf("unknown session listed %+v", none)
	}

	first, err := s.ListPlans(ctx, store.PlanQuery{PageQuery: store.PageQuery{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Next == "" {
		t.Fatalf("page 1 = %d items, next %q", len(first.Items), first.Next)
	}
	second, err := s.ListPlans(ctx, store.PlanQuery{PageQuery: store.PageQuery{Limit: 2, Cursor: first.Next}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 2 || second.Next != "" {
		t.Fatalf("page 2 = %d items, next %q", len(second.Items), second.Next)
	}
	seen := map[string]bool{}
	for _, p := range append(first.Items, second.Items...) {
		if seen[p.ID.String()] {
			t.Fatalf("cursor repeated %s", p.ID)
		}
		seen[p.ID.String()] = true
	}
	if len(seen) != 4 {
		t.Fatalf("paged over %d plans, want 4", len(seen))
	}

	if _, err := s.ListPlans(ctx, store.PlanQuery{PageQuery: store.PageQuery{Cursor: "not-a-cursor"}}); err == nil {
		t.Fatal("bad cursor accepted")
	}
}

func TestApprovalRoundTripAndValidation(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	p := fullPlan(t, sid, "p1", created)
	if err := s.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}

	want := store.Approval{
		ID:        mustApID(t, "ap1"),
		Kind:      "plan",
		Status:    "pending",
		PlanID:    p.ID,
		SessionID: sid,
		Summary:   "pass: seeded plan",
		CreatedAt: created,
	}
	if err := s.CreateApproval(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetApproval(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Kind != want.Kind || got.Status != want.Status {
		t.Fatalf("scalars %+v", got)
	}
	if got.PlanID != want.PlanID || got.SessionID != want.SessionID || got.Summary != want.Summary {
		t.Fatalf("refs %+v", got)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("created_at %s want %s", got.CreatedAt, want.CreatedAt)
	}

	want.Status = "approved"
	want.Summary = "decided"
	if err := s.UpdateApproval(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, err = s.GetApproval(ctx, want.ID); err != nil || got.Status != "approved" || got.Summary != "decided" {
		t.Fatalf("after update %+v err=%v", got, err)
	}

	if _, err := s.GetApproval(ctx, store.ApprovalID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("get zero id err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetApproval(ctx, mustApID(t, "ap_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get absent err = %v, want ErrNotFound", err)
	}
	absent := want
	absent.ID = mustApID(t, "ap_absent")
	if err := s.UpdateApproval(ctx, absent); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update absent err = %v, want ErrNotFound", err)
	}

	bad := []struct {
		name string
		mut  func(a *store.Approval)
	}{
		{"zero id", func(a *store.Approval) { a.ID = store.ApprovalID{} }},
		{"empty kind", func(a *store.Approval) { a.Kind = "" }},
		{"empty status", func(a *store.Approval) { a.Status = "" }},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := want
			tc.mut(&a)
			if err := s.CreateApproval(ctx, a); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("create err = %v, want ErrInvalid", err)
			}
			if err := s.UpdateApproval(ctx, a); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("update err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestListApprovalsPendingIsOldestFirst(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	// The inbox reads oldest pending first; history reads newest first. One
	// method serves both, so the ordering flip is the thing worth pinning.
	for i, id := range []string{"ap1", "ap2", "ap3"} {
		a := store.Approval{
			ID: mustApID(t, id), Kind: "plan", Status: "pending",
			SessionID: sid, Summary: id, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := s.CreateApproval(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	decided := store.Approval{
		ID: mustApID(t, "ap4"), Kind: "plan", Status: "approved",
		SessionID: sid, Summary: "ap4", CreatedAt: base.Add(3 * time.Minute),
	}
	if err := s.CreateApproval(ctx, decided); err != nil {
		t.Fatal(err)
	}

	pending, err := s.ListApprovals(ctx, store.ApprovalQuery{Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Items) != 3 {
		t.Fatalf("pending listed %d, want 3", len(pending.Items))
	}
	if pending.Items[0].ID.String() != "ap1" || pending.Items[2].ID.String() != "ap3" {
		t.Fatalf("pending not oldest first: %s..%s", pending.Items[0].ID, pending.Items[2].ID)
	}

	all, err := s.ListApprovals(ctx, store.ApprovalQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 4 || all.Items[0].ID.String() != "ap4" {
		t.Fatalf("history not newest first: %+v", all.Items)
	}

	first, err := s.ListApprovals(ctx, store.ApprovalQuery{PageQuery: store.PageQuery{Limit: 2}, Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Next == "" {
		t.Fatalf("page 1 = %d items, next %q", len(first.Items), first.Next)
	}
	second, err := s.ListApprovals(ctx, store.ApprovalQuery{PageQuery: store.PageQuery{Limit: 2, Cursor: first.Next}, Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Next != "" {
		t.Fatalf("page 2 = %d items, next %q", len(second.Items), second.Next)
	}
	if second.Items[0].ID.String() != "ap3" {
		t.Fatalf("cursor skipped: %s", second.Items[0].ID)
	}

	if _, err := s.ListApprovals(ctx, store.ApprovalQuery{PageQuery: store.PageQuery{Cursor: "not-a-cursor"}}); err == nil {
		t.Fatal("bad cursor accepted")
	}
}
