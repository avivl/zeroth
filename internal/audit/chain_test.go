package audit

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/signer"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

func TestCanonicalStable(t *testing.T) {
	t.Parallel()
	ts := time.Unix(1_700_000_000, 0).UTC()
	p := Payload{Action: "run.create", Target: "s_1", Approver: "operator", AgentPubKey: "aa", Timestamp: ts}
	a := Canonical(p)
	b := Canonical(p)
	if string(a) != string(b) {
		t.Fatal("canonical encoding is not stable")
	}
	p.Action = "run.other"
	if string(Canonical(p)) == string(a) {
		t.Fatal("action change must change canonical bytes")
	}
}

func TestChainCleanAndTamper(t *testing.T) {
	t.Parallel()
	st, log, agent, sess := readyLog(t)
	ctx := t.Context()
	if _, err := log.Append(ctx, Entry{
		Action: ActionRunCreate, Target: sess.String(), AgentID: agent,
		SessionID: sess, ResourceType: "run", ResourceID: sess.String(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(ctx, Entry{
		Action: ActionPlanApply, Target: "plan-1", PlanHash: "ph", AgentID: agent,
		SessionID: sess, ResourceType: "plan", ResourceID: "plan-1",
	}); err != nil {
		t.Fatal(err)
	}
	chain, keys := load(t, st)
	if err := VerifyChain(chain, keys); err != nil {
		t.Fatalf("clean chain: %v", err)
	}
	if err := VerifyRecord(chain[1], keys); err != nil {
		t.Fatalf("independent record: %v", err)
	}

	tampered := chain[1]
	tampered.Action = "plan.evil"
	err := VerifyChain([]store.AuditRecord{chain[0], tampered}, keys)
	if err == nil {
		t.Fatal("tamper verified")
	}
	var fail Failure
	if !errors.As(err, &fail) || fail.Record != tampered.ID {
		t.Fatalf("tamper must name the record: %v", err)
	}
	if !strings.Contains(err.Error(), tampered.ID.String()) {
		t.Fatalf("error should name record: %v", err)
	}
}

func TestMissingMiddleBreaksChain(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "zeroth.db")
	st, err := sqlite.New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sg := signer.NewMemory()
	log, err := NewLog(st, sg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	agent := seedAgent(t, st, "a_mid")
	if err := log.EnsureAgentKey(ctx, agent, true); err != nil {
		t.Fatal(err)
	}
	sess := seedSess(t, st, agent, "s_mid")
	var ids []store.AuditID
	for _, action := range []string{"run.create", "plan.apply", "agent.update"} {
		rec, err := log.Append(ctx, Entry{
			Action: action, Target: sess.String(), AgentID: agent,
			SessionID: sess, ResourceType: "run", ResourceID: sess.String(),
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ID)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS audit_no_delete`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM audit_records WHERE id = ?`, ids[1].String()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st2, err := sqlite.New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	chain, keys := load(t, st2)
	err = VerifyChain(chain, keys)
	if err == nil {
		t.Fatal("gap verified")
	}
	var fail Failure
	if !errors.As(err, &fail) {
		t.Fatalf("want Failure, got %v", err)
	}
	if fail.Record != ids[2] {
		t.Fatalf("should name the record after the gap, got %s want %s (%v)", fail.Record, ids[2], err)
	}
	if !strings.Contains(err.Error(), "chain broken") || !strings.Contains(err.Error(), ids[2].String()) {
		t.Fatalf("should say where: %v", err)
	}
}

func TestKeyRotationKeepsHistory(t *testing.T) {
	t.Parallel()
	st, log, agent, sess := readyLog(t)
	ctx := t.Context()
	first, err := log.Append(ctx, Entry{
		Action: ActionRunCreate, Target: sess.String(), AgentID: agent,
		SessionID: sess, ResourceType: "run", ResourceID: sess.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.RotateAgentKey(ctx, agent); err != nil {
		t.Fatal(err)
	}
	second, err := log.Append(ctx, Entry{
		Action: ActionAgentUpdate, Target: agent.String(), AgentID: agent,
		ResourceType: "agent", ResourceID: agent.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.AgentPubKey == second.AgentPubKey {
		t.Fatal("expected new pubkey after rotate")
	}
	chain, keys := load(t, st)
	if len(keys) < 2 {
		t.Fatalf("registry should keep both keys, got %d", len(keys))
	}
	if err := VerifyChain(chain, keys); err != nil {
		t.Fatalf("after rotate: %v", err)
	}
	if err := VerifyRecord(first, keys); err != nil {
		t.Fatalf("historical record: %v", err)
	}
}

func TestVerifyUnknownKeyRejected(t *testing.T) {
	t.Parallel()
	st, log, agent, sess := readyLog(t)
	ctx := t.Context()
	rec, err := log.Append(ctx, Entry{
		Action: ActionRunCreate, Target: sess.String(), AgentID: agent,
		SessionID: sess, ResourceType: "run", ResourceID: sess.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecord(rec, nil); err == nil {
		t.Fatal("empty registry accepted")
	}
	_ = st
}

func readyLog(t *testing.T) (store.Store, *Log, store.AgentID, store.SessionID) {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sg := signer.NewMemory()
	log, err := NewLog(st, sg)
	if err != nil {
		t.Fatal(err)
	}
	agent := seedAgent(t, st, "a_test")
	if err := log.EnsureAgentKey(t.Context(), agent, true); err != nil {
		t.Fatal(err)
	}
	sess := seedSess(t, st, agent, "s_test")
	return st, log, agent, sess
}

func load(t *testing.T, st store.Store) ([]store.AuditRecord, []store.AgentKey) {
	t.Helper()
	chain, err := st.AuditChain(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListAgentKeys(t.Context(), store.AgentID{})
	if err != nil {
		t.Fatal(err)
	}
	return chain, keys
}

func seedAgent(t *testing.T, st store.Store, id string) store.AgentID {
	t.Helper()
	aid, err := store.ParseAgentID(id)
	if err != nil {
		t.Fatal(err)
	}
	err = st.CreateAgent(t.Context(), store.Agent{
		ID: aid, Name: "n", Harness: "h", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	return aid
}

func seedSess(t *testing.T, st store.Store, agent store.AgentID, id string) store.SessionID {
	t.Helper()
	sid, err := store.ParseSessionID(id)
	if err != nil {
		t.Fatal(err)
	}
	err = st.CreateSession(t.Context(), store.Session{
		ID: sid, AgentID: agent, Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	return sid
}
