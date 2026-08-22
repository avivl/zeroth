package store_test

import (
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
)

type harness struct {
	name string
	open func(t *testing.T) store.Store
}

func TestStoreConformance(t *testing.T) {
	t.Parallel()

	// Adding a backend at stage 2 is one more row. The cases below must
	// stay implementation-agnostic (NFR-4).
	cases := []harness{
		{
			name: "sqlite",
			open: func(t *testing.T) store.Store {
				t.Helper()
				s, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
				if err != nil {
					t.Fatalf("sqlite.New: %v", err)
				}
				t.Cleanup(func() {
					if err := s.Close(); err != nil {
						t.Errorf("Close: %v", err)
					}
				})
				return s
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := tc.open(t)
			if got := s.Name(); got != tc.name {
				t.Fatalf("Name() = %q, want %q", got, tc.name)
			}

			t.Run("agents", func(t *testing.T) { testAgents(t, tc.open) })
			t.Run("sessions", func(t *testing.T) { testSessions(t, tc.open) })
			t.Run("events", func(t *testing.T) { testEvents(t, tc.open) })
			t.Run("plans", func(t *testing.T) { testPlans(t, tc.open) })
			t.Run("approvals", func(t *testing.T) { testApprovals(t, tc.open) })
			t.Run("memory", func(t *testing.T) { testMemory(t, tc.open) })
			t.Run("audit", func(t *testing.T) { testAudit(t, tc.open) })
			t.Run("agent_keys", func(t *testing.T) { testAgentKeys(t, tc.open) })
			t.Run("leases", func(t *testing.T) { testLeases(t, tc.open) })
			t.Run("checkpoints", func(t *testing.T) { testCheckpoints(t, tc.open) })
			t.Run("close", func(t *testing.T) { testClose(t, tc.open) })
		})
	}
}

func testClose(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close again: %v", err)
	}
	if _, err := s.GetAgent(t.Context(), mustAgentID(t, "gone")); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("after Close: %v, want ErrClosed", err)
	}
}

func testAgents(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	a := store.Agent{
		ID:           mustAgentID(t, "agent-1"),
		Name:         "claude",
		Harness:      "claudecode",
		Status:       "ready",
		Model:        "claude-opus",
		Tools:        []string{"bash", "read"},
		AutonomyTier: "t1",
		CreatedAt:    ts(1),
		UpdatedAt:    ts(1),
	}
	if err := s.CreateAgent(ctx, a); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	got, err := s.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.Name != a.Name || got.Harness != a.Harness || len(got.Tools) != 2 || got.Tools[0] != "bash" {
		t.Fatalf("GetAgent = %+v", got)
	}
	if err := s.CreateAgent(ctx, a); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate CreateAgent: %v, want ErrConflict", err)
	}
	a.Status = "busy"
	a.UpdatedAt = ts(2)
	if err := s.UpdateAgent(ctx, a); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	got, err = s.GetAgent(ctx, a.ID)
	if err != nil || got.Status != "busy" {
		t.Fatalf("updated agent: %+v err=%v", got, err)
	}
	if _, err := s.GetAgent(ctx, mustAgentID(t, "missing")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	missing := a
	missing.ID = mustAgentID(t, "nope")
	if err := s.UpdateAgent(ctx, missing); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}

	for i := 2; i <= 4; i++ {
		n := store.Agent{
			ID:        mustAgentID(t, "agent-"+itoa(i)),
			Name:      "n",
			Harness:   "claudecode",
			Status:    "ready",
			CreatedAt: ts(int64(i)),
			UpdatedAt: ts(int64(i)),
		}
		if err := s.CreateAgent(ctx, n); err != nil {
			t.Fatalf("CreateAgent %d: %v", i, err)
		}
	}
	page, err := s.ListAgents(ctx, store.PageQuery{Limit: 2})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(page.Items) != 2 || page.Next == "" {
		t.Fatalf("page = %+v", page)
	}
	if page.Items[0].ID.String() != "agent-4" {
		t.Fatalf("newest first: %s", page.Items[0].ID)
	}
	page2, err := s.ListAgents(ctx, store.PageQuery{Limit: 2, Cursor: page.Next})
	if err != nil {
		t.Fatalf("ListAgents page2: %v", err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("page2 len=%d", len(page2.Items))
	}
}

func testSessions(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	agent := seedAgent(t, s, "agent-s")
	sess := store.Session{
		ID:           mustSessionID(t, "sess-1"),
		AgentID:      agent.ID,
		Status:       "running",
		Prompt:       "do the thing",
		TrackerRef:   "42-19",
		Workspace:    store.WorkspaceSource{Repo: "https://example.com/r.git", Ref: "main"},
		AutonomyTier: "t1",
		CreatedAt:    ts(1),
		UpdatedAt:    ts(1),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Prompt != sess.Prompt || got.Workspace.Repo != sess.Workspace.Repo || got.TrackerRef != "42-19" {
		t.Fatalf("GetSession = %+v", got)
	}
	sess.Status = "backgrounded"
	sess.UpdatedAt = ts(2)
	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	got, err = s.GetSession(ctx, sess.ID)
	if err != nil || got.Status != "backgrounded" {
		t.Fatalf("updated: %+v err=%v", got, err)
	}

	other := sess
	other.ID = mustSessionID(t, "sess-2")
	other.Status = "stopped"
	other.CreatedAt = ts(3)
	other.UpdatedAt = ts(3)
	other.FinishedAt = ts(4)
	if err := s.CreateSession(ctx, other); err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	page, err := s.ListSessions(ctx, store.SessionQuery{Status: "stopped", PageQuery: store.PageQuery{Limit: 10}})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID.String() != "sess-2" || page.Items[0].FinishedAt.IsZero() {
		t.Fatalf("filter stopped: %+v", page.Items)
	}
	page, err = s.ListSessions(ctx, store.SessionQuery{AgentID: agent.ID, PageQuery: store.PageQuery{Limit: 10}})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("filter agent: %+v err=%v", page, err)
	}
}

func testEvents(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	sess := seedSession(t, s, "ev-agent", "ev-sess")

	if _, err := s.AppendEvents(ctx, sess.ID, nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("empty batch: %v", err)
	}

	var seqs []int64
	for i := 0; i < 5; i++ {
		ev, err := s.AppendEvent(ctx, sess.ID, store.Event{Type: "log", Message: itoa(i), Payload: "p"})
		if err != nil {
			t.Fatalf("AppendEvent %d: %v", i, err)
		}
		if ev.Seq == 0 || ev.ID.IsZero() || ev.SessionID != sess.ID || ev.Payload != "p" {
			t.Fatalf("event = %+v", ev)
		}
		if len(seqs) > 0 && ev.Seq <= seqs[len(seqs)-1] {
			t.Fatalf("seq not increasing: %v then %d", seqs, ev.Seq)
		}
		seqs = append(seqs, ev.Seq)
	}

	last, err := s.ReplayLast(ctx, sess.ID, 3)
	if err != nil {
		t.Fatalf("ReplayLast: %v", err)
	}
	if len(last) != 3 || last[0].Message != "2" || last[2].Message != "4" {
		t.Fatalf("replay = %+v", last)
	}
	if last[0].Seq >= last[1].Seq || last[1].Seq >= last[2].Seq {
		t.Fatalf("replay not chronological: %+v", last)
	}

	after, err := s.EventsAfter(ctx, sess.ID, last[0].Seq)
	if err != nil {
		t.Fatalf("EventsAfter: %v", err)
	}
	if len(after) != 2 || after[0].Message != "3" {
		t.Fatalf("after = %+v", after)
	}

	other := seedSession(t, s, "ev-agent-2", "ev-sess-2")
	if _, err := s.AppendEvent(ctx, other.ID, store.Event{Type: "log", Message: "other"}); err != nil {
		t.Fatalf("other append: %v", err)
	}
	own, err := s.ReplayLast(ctx, sess.ID, 50)
	if err != nil || len(own) != 5 {
		t.Fatalf("session isolation: len=%d err=%v", len(own), err)
	}

	batched, err := s.AppendEvents(ctx, sess.ID, []store.Event{
		{Type: "log", Message: "b1"},
		{Type: "log", Message: "b2"},
	})
	if err != nil || len(batched) != 2 || batched[1].Seq <= batched[0].Seq {
		t.Fatalf("batch = %+v err=%v", batched, err)
	}

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := s.AppendEvent(ctx, sess.ID, store.Event{Type: "log", Message: "race"})
			if err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent append: %v", err)
	}
	all, err := s.ReplayLast(ctx, sess.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5+2+n {
		t.Fatalf("len=%d, want %d", len(all), 5+2+n)
	}
}

func testPlans(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	sess := seedSession(t, s, "plan-agent", "plan-sess")
	p := store.Plan{
		ID:          mustPlanID(t, "plan-1"),
		SessionID:   sess.ID,
		Status:      "draft",
		Summary:     "touch README",
		Hash:        "abc123",
		ExpiresAt:   ts(10),
		CostCeiling: 1000,
		ScopeID:     mustScopeID(t, "scope-a"),
		Credentials: []store.CredentialConstraint{{Provider: "anthropic", Kind: "api_key"}},
		Effects: []store.PlanEffect{{
			Type:              "modify",
			Path:              "README.md",
			Diff:              "+hi",
			PreconditionHash:  "abc",
			PostconditionHash: "def",
			IdempotencyKey:    "idem-1",
			LeaseID:           mustLeaseID(t, "lease-1"),
		}},
		CrossExam: &store.CrossExam{
			Verdict:       "ok",
			ReviewerModel: "opus",
			Reasoning:     "looks fine",
			At:            ts(2),
		},
		SecretScanFindings: []store.SecretScanFinding{},
		CreatedAt:          ts(1),
		UpdatedAt:          ts(1),
	}
	if err := s.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	got, err := s.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if len(got.Effects) != 1 || got.Effects[0].Path != "README.md" || got.CrossExam == nil || got.CrossExam.Verdict != "ok" {
		t.Fatalf("GetPlan = %+v", got)
	}
	if got.Hash != "abc123" || got.CostCeiling != 1000 || got.ScopeID.String() != "scope-a" {
		t.Fatalf("plan constraints: %+v", got)
	}
	if got.Effects[0].LeaseID.String() != "lease-1" || got.Effects[0].IdempotencyKey != "idem-1" || got.Effects[0].PostconditionHash != "def" {
		t.Fatalf("plan row: %+v", got.Effects[0])
	}
	if len(got.Credentials) != 1 || got.Credentials[0].Provider != "anthropic" {
		t.Fatalf("credentials: %+v", got.Credentials)
	}
	p.Status = "approved"
	p.ReviewComment = "ship it"
	p.UpdatedAt = ts(3)
	if err := s.UpdatePlan(ctx, p); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	got, err = s.GetPlan(ctx, p.ID)
	if err != nil || got.Status != "approved" || got.ReviewComment != "ship it" {
		t.Fatalf("updated plan: %+v err=%v", got, err)
	}

	p2 := p
	p2.ID = mustPlanID(t, "plan-2")
	p2.Status = "draft"
	p2.CreatedAt = ts(4)
	p2.UpdatedAt = ts(4)
	p2.CrossExam = nil
	if err := s.CreatePlan(ctx, p2); err != nil {
		t.Fatalf("CreatePlan 2: %v", err)
	}
	page, err := s.ListPlans(ctx, store.PlanQuery{SessionID: sess.ID, Status: "draft", PageQuery: store.PageQuery{Limit: 10}})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID.String() != "plan-2" {
		t.Fatalf("list draft: %+v err=%v", page, err)
	}
}

func testApprovals(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	sess := seedSession(t, s, "ap-agent", "ap-sess")
	for i, st := range []string{"pending", "pending", "approved"} {
		a := store.Approval{
			ID:        mustApprovalID(t, "ap-"+itoa(i+1)),
			Kind:      "plan",
			Status:    st,
			SessionID: sess.ID,
			Summary:   "item",
			CreatedAt: ts(int64(i + 1)),
		}
		if err := s.CreateApproval(ctx, a); err != nil {
			t.Fatalf("CreateApproval: %v", err)
		}
	}
	page, err := s.ListApprovals(ctx, store.ApprovalQuery{Status: "pending", PageQuery: store.PageQuery{Limit: 10}})
	if err != nil {
		t.Fatalf("ListApprovals: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].ID.String() != "ap-1" || page.Items[1].ID.String() != "ap-2" {
		t.Fatalf("oldest pending first: %+v", page.Items)
	}
	first := page.Items[0]
	first.Status = "approved"
	if err := s.UpdateApproval(ctx, first); err != nil {
		t.Fatalf("UpdateApproval: %v", err)
	}
	got, err := s.GetApproval(ctx, first.ID)
	if err != nil || got.Status != "approved" {
		t.Fatalf("get: %+v err=%v", got, err)
	}
}

func testMemory(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	sess := seedSession(t, s, "mem-agent", "mem-sess")
	m := store.MemoryEntry{
		ID:        mustMemoryID(t, "mem-1"),
		Kind:      "session",
		RefID:     sess.ID.String(),
		Content:   "prefer table tests",
		CreatedAt: ts(1),
	}
	if err := s.CreateMemory(ctx, m); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	got, err := s.GetMemory(ctx, m.ID)
	if err != nil || got.Content != m.Content {
		t.Fatalf("GetMemory: %+v err=%v", got, err)
	}
	page, err := s.ListMemory(ctx, store.MemoryQuery{Kind: "session", RefID: sess.ID.String(), PageQuery: store.PageQuery{Limit: 10}})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("ListMemory: %+v err=%v", page, err)
	}

	prop := store.MemoryProposal{
		ID:        mustProposalID(t, "prop-1"),
		Kind:      "session",
		RefID:     sess.ID.String(),
		SessionID: sess.ID,
		Content:   "remember the kernel",
		Status:    "pending",
		CreatedAt: ts(2),
	}
	if err := s.CreateMemoryProposal(ctx, prop); err != nil {
		t.Fatalf("CreateMemoryProposal: %v", err)
	}
	prop.Status = "accepted"
	prop.MemoryID = m.ID
	prop.ReviewedAt = ts(3)
	if err := s.UpdateMemoryProposal(ctx, prop); err != nil {
		t.Fatalf("UpdateMemoryProposal: %v", err)
	}
	pgot, err := s.GetMemoryProposal(ctx, prop.ID)
	if err != nil || pgot.Status != "accepted" || pgot.MemoryID != m.ID || pgot.ReviewedAt.IsZero() {
		t.Fatalf("proposal: %+v err=%v", pgot, err)
	}
}

func testAudit(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	sess := seedSession(t, s, "aud-agent", "aud-sess")
	r := store.AuditRecord{
		ID:           mustAuditID(t, "aud-1"),
		Action:       "plan.apply",
		Target:       "plan-1",
		PlanHash:     "ph",
		Approver:     "operator",
		AgentPubKey:  "11",
		PrevHash:     "",
		Hash:         "h1",
		ResourceType: "plan",
		ResourceID:   "plan-1",
		Actor:        "operator",
		Signature:    "sig",
		AgentID:      sess.AgentID,
		SessionID:    sess.ID,
		CreatedAt:    ts(1),
	}
	got, err := s.AppendAudit(ctx, r)
	if err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}
	if got.CreatedAt.IsZero() || got.Hash != "h1" || got.Target != "plan-1" {
		t.Fatalf("AppendAudit got %+v", got)
	}
	if _, err := s.AppendAudit(ctx, r); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate audit: %v", err)
	}
	fetched, err := s.GetAudit(ctx, r.ID)
	if err != nil || fetched.Action != r.Action || fetched.Signature != "sig" || fetched.Hash != "h1" || fetched.AgentPubKey != "11" {
		t.Fatalf("GetAudit: %+v err=%v", fetched, err)
	}
	r2 := r
	r2.ID = mustAuditID(t, "aud-2")
	r2.ResourceID = "plan-2"
	r2.Target = "plan-2"
	r2.PrevHash = "h1"
	r2.Hash = "h2"
	r2.CreatedAt = ts(2)
	if _, err := s.AppendAudit(ctx, r2); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListAudit(ctx, store.AuditQuery{ResourceType: "plan", ResourceID: "plan-1", PageQuery: store.PageQuery{Limit: 10}})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID.String() != "aud-1" {
		t.Fatalf("list: %+v err=%v", page, err)
	}
	byRun, err := s.ListAudit(ctx, store.AuditQuery{SessionID: sess.ID, PageQuery: store.PageQuery{Limit: 10}})
	if err != nil || len(byRun.Items) != 2 {
		t.Fatalf("list by session: %+v err=%v", byRun, err)
	}
	chain, err := s.AuditChain(ctx)
	if err != nil || len(chain) != 2 || chain[0].ID.String() != "aud-1" || chain[1].PrevHash != "h1" {
		t.Fatalf("chain: %+v err=%v", chain, err)
	}
}

func testAgentKeys(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	agent := seedAgent(t, s, "key-agent")
	k1 := store.AgentKey{AgentID: agent.ID, PubKey: "aa", CreatedAt: ts(1)}
	if err := s.AppendAgentKey(ctx, k1); err != nil {
		t.Fatalf("AppendAgentKey: %v", err)
	}
	if err := s.AppendAgentKey(ctx, k1); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate key: %v", err)
	}
	k2 := store.AgentKey{AgentID: agent.ID, PubKey: "bb", CreatedAt: ts(2)}
	if err := s.AppendAgentKey(ctx, k2); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAgentKeys(ctx, agent.ID)
	if err != nil || len(got) != 2 || got[0].PubKey != "aa" || got[1].PubKey != "bb" {
		t.Fatalf("list keys: %+v err=%v", got, err)
	}
	all, err := s.ListAgentKeys(ctx, store.AgentID{})
	if err != nil || len(all) != 2 {
		t.Fatalf("list all keys: %+v err=%v", all, err)
	}
}

func testLeases(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	agent := seedAgent(t, s, "lease-agent")
	l := store.Lease{
		ID:        mustLeaseID(t, "lease-1"),
		GrantID:   mustGrantID(t, "grant-1"),
		ScopeID:   mustScopeID(t, "scope-1"),
		AgentID:   agent.ID,
		ExpiresAt: ts(10),
		MintedAt:  ts(1),
	}
	if err := s.CreateLease(ctx, l); err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	l.ExpiresAt = ts(20)
	if err := s.UpdateLease(ctx, l); err != nil {
		t.Fatalf("UpdateLease: %v", err)
	}
	got, err := s.GetLease(ctx, l.ID)
	if err != nil || !got.ExpiresAt.Equal(ts(20)) || got.GrantID != l.GrantID {
		t.Fatalf("GetLease: %+v err=%v", got, err)
	}
	page, err := s.ListLeases(ctx, store.LeaseQuery{AgentID: agent.ID, PageQuery: store.PageQuery{Limit: 10}})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("ListLeases: %+v err=%v", page, err)
	}
}

func testCheckpoints(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	s := open(t)
	ctx := t.Context()
	sess := seedSession(t, s, "ck-agent", "ck-sess")
	c := store.Checkpoint{
		ID:        mustCheckpointID(t, "ck-1"),
		SessionID: sess.ID,
		Label:     "before apply",
		Location:  "/var/zeroth/ck-1.tar",
		CreatedAt: ts(1),
	}
	if err := s.CreateCheckpoint(ctx, c); err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	got, err := s.GetCheckpoint(ctx, c.ID)
	if err != nil || got.Location != c.Location || got.Label != c.Label {
		t.Fatalf("GetCheckpoint: %+v err=%v", got, err)
	}
	page, err := s.ListCheckpoints(ctx, store.CheckpointQuery{SessionID: sess.ID, PageQuery: store.PageQuery{Limit: 10}})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("ListCheckpoints: %+v err=%v", page, err)
	}
}

func seedAgent(t *testing.T, s store.Store, id string) store.Agent {
	t.Helper()
	a := store.Agent{
		ID:        mustAgentID(t, id),
		Name:      id,
		Harness:   "claudecode",
		Status:    "ready",
		CreatedAt: ts(1),
		UpdatedAt: ts(1),
	}
	if err := s.CreateAgent(t.Context(), a); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return a
}

func seedSession(t *testing.T, s store.Store, agentID, sessionID string) store.Session {
	t.Helper()
	agent := seedAgent(t, s, agentID)
	sess := store.Session{
		ID:        mustSessionID(t, sessionID),
		AgentID:   agent.ID,
		Status:    "running",
		CreatedAt: ts(1),
		UpdatedAt: ts(1),
	}
	if err := s.CreateSession(t.Context(), sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return sess
}

func ts(n int64) time.Time { return time.Unix(n, 0).UTC() }

func itoa(i int) string { return strconv.Itoa(i) }

func mustAgentID(t *testing.T, raw string) store.AgentID {
	t.Helper()
	id, err := store.ParseAgentID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustSessionID(t *testing.T, raw string) store.SessionID {
	t.Helper()
	id, err := store.ParseSessionID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustPlanID(t *testing.T, raw string) store.PlanID {
	t.Helper()
	id, err := store.ParsePlanID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustApprovalID(t *testing.T, raw string) store.ApprovalID {
	t.Helper()
	id, err := store.ParseApprovalID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustMemoryID(t *testing.T, raw string) store.MemoryID {
	t.Helper()
	id, err := store.ParseMemoryID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustProposalID(t *testing.T, raw string) store.MemoryProposalID {
	t.Helper()
	id, err := store.ParseMemoryProposalID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustAuditID(t *testing.T, raw string) store.AuditID {
	t.Helper()
	id, err := store.ParseAuditID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustLeaseID(t *testing.T, raw string) store.LeaseID {
	t.Helper()
	id, err := store.ParseLeaseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustGrantID(t *testing.T, raw string) store.GrantID {
	t.Helper()
	id, err := store.ParseGrantID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustScopeID(t *testing.T, raw string) store.ScopeID {
	t.Helper()
	id, err := store.ParseScopeID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func mustCheckpointID(t *testing.T, raw string) store.CheckpointID {
	t.Helper()
	id, err := store.ParseCheckpointID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
