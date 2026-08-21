package plan_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
)

func TestPlanRoundTripRendersIdentically(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	agent := store.Agent{ID: mustAgent(t, "agent-1"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	sessID, err := session.ParseID("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	storeSess, err := store.ParseSessionID(sessID.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, store.Session{ID: storeSess, AgentID: agent.ID, Status: "running"}); err != nil {
		t.Fatal(err)
	}

	id, err := plan.ParseID("plan-1")
	if err != nil {
		t.Fatal(err)
	}
	original, err := plan.Build(plan.Draft{
		ID:        id,
		SessionID: sessID,
		Summary:   "touch README and greet.go",
		Effects: []plan.Proposed{
			{Type: "modify", Path: "README.md", Diff: "+Version: 2"},
			{Type: "modify", Path: "greet.go", Diff: "+Greet"},
			{Type: "create", Path: "new.txt", Diff: "hello"},
		},
		Observed: map[string]string{
			"README.md": "hash-readme",
			"greet.go":  "hash-greet",
		},
		Lease:       "lease-1",
		ExpiresAt:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		CostCeiling: 4200,
		Scope:       "scope-a",
		Credentials: []plan.Credential{{Provider: "anthropic", Kind: "api_key"}},
		Now:         time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := original.Render()

	rec, err := original.Record()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePlan(ctx, rec); err != nil {
		t.Fatal(err)
	}
	gotRec, err := s.GetPlan(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := plan.FromRecord(gotRec)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Hash != original.Hash {
		t.Fatalf("hash %s vs %s", loaded.Hash, original.Hash)
	}
	if loaded.Render() != want {
		t.Fatalf("render mismatch\nwant:\n%s\ngot:\n%s", want, loaded.Render())
	}

	// Serialization through the store must not change the digest, including
	// when independent rows are loaded back in draft order.
	if plan.HashOf(loaded) != original.Hash {
		t.Fatal("canonical hash changed across store round-trip")
	}
}

func TestFromRecordRejectsTamperedHash(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	agent := store.Agent{ID: mustAgent(t, "agent-1"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: mustSess(t, "sess-1"), AgentID: agent.ID, Status: "running"}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	rec := store.Plan{
		ID:        mustPlan(t, "plan-1"),
		SessionID: sess.ID,
		Status:    "draft",
		Summary:   "x",
		Hash:      "deadbeef",
		ExpiresAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		ScopeID:   mustScope(t, "scope-a"),
		Effects: []store.PlanEffect{{
			Type: "modify", Path: "a.md", Diff: "x", PreconditionHash: "p",
			LeaseID: mustLease(t, "lease-1"), IdempotencyKey: "i", PostconditionHash: "q",
		}},
	}
	if err := s.CreatePlan(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPlan(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.FromRecord(got); err == nil {
		t.Fatal("expected hash mismatch")
	}
}

func mustAgent(t *testing.T, raw string) store.AgentID {
	t.Helper()
	id, err := store.ParseAgentID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSess(t *testing.T, raw string) store.SessionID {
	t.Helper()
	id, err := store.ParseSessionID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustPlan(t *testing.T, raw string) store.PlanID {
	t.Helper()
	id, err := store.ParsePlanID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustScope(t *testing.T, raw string) store.ScopeID {
	t.Helper()
	id, err := store.ParseScopeID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustLease(t *testing.T, raw string) store.LeaseID {
	t.Helper()
	id, err := store.ParseLeaseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
