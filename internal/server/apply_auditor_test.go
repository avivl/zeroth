package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/signer"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
)

func TestApplyAuditorSignRowProducesVerifiableSignature(t *testing.T) {
	t.Parallel()
	st, log, agent, sess := readyApplyAudit(t)
	aud := &applyAuditor{
		log:     log,
		agent:   agent,
		session: sess,
		planID:  "plan-row-sign",
		hash:    "planhash-deadbeef",
	}
	row := plan.Row{
		Op:             plan.OpModify,
		Target:         "docs/design/plan.md",
		Payload:        "-typo\n+fixed",
		Lease:          "lease-1",
		Precondition:   "pre-abc",
		IdempotencyKey: "idem-row-1",
		Postcondition:  "post-xyz",
	}
	post := "post-observed"
	if err := aud.SignRow(t.Context(), row, post); err != nil {
		t.Fatal(err)
	}

	chain, err := st.AuditChain(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var rec store.AuditRecord
	for _, item := range chain {
		if item.Action == audit.ActionPlanApplyRow {
			rec = item
			break
		}
	}
	if rec.ID.IsZero() {
		t.Fatal("SignRow did not append a plan.apply.row record")
	}
	if rec.Signature == "" {
		t.Fatal("row record has an empty signature")
	}
	if rec.Action != audit.ActionPlanApplyRow {
		t.Fatalf("action %q", rec.Action)
	}
	if rec.Target != row.Target {
		t.Fatalf("target %q", rec.Target)
	}
	if rec.PlanHash != aud.hash {
		t.Fatalf("plan hash %q", rec.PlanHash)
	}
	if rec.Precondition != row.Precondition {
		t.Fatalf("precondition %q", rec.Precondition)
	}
	if rec.Postcondition != post {
		t.Fatalf("postcondition %q", rec.Postcondition)
	}
	if rec.LeaseID.String() != string(row.Lease) {
		t.Fatalf("lease %q", rec.LeaseID)
	}
	if rec.ResourceType != "plan" || rec.ResourceID != aud.planID {
		t.Fatalf("resource %s/%s", rec.ResourceType, rec.ResourceID)
	}

	payload := audit.Payload{
		Action:        rec.Action,
		Target:        rec.Target,
		PlanHash:      rec.PlanHash,
		Precondition:  rec.Precondition,
		Postcondition: rec.Postcondition,
		LeaseID:       rec.LeaseID.String(),
		Approver:      rec.Approver,
		AgentPubKey:   rec.AgentPubKey,
		PrevHash:      rec.PrevHash,
		Timestamp:     rec.CreatedAt,
	}
	pub, err := signer.ParsePublicKey(rec.AgentPubKey)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := signer.ParseSignature(rec.Signature)
	if err != nil {
		t.Fatal(err)
	}
	dig := audit.Digest(payload)
	if err := signer.Verify(pub, dig[:], sig); err != nil {
		t.Fatalf("independent Schnorr verify: %v", err)
	}

	keys, err := st.ListAgentKeys(t.Context(), agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.VerifyRecord(rec, keys); err != nil {
		t.Fatalf("VerifyRecord: %v", err)
	}

	tampered := rec
	tampered.Target = "docs/evil.md"
	if err := audit.VerifyRecord(tampered, keys); err == nil {
		t.Fatal("tampered row record verified")
	}
}

func TestApplyAuditorSignRowFailClosed(t *testing.T) {
	t.Parallel()
	_, log, agent, sess := readyApplyAudit(t)
	base := applyAuditor{log: log, agent: agent, session: sess, planID: "plan-1", hash: "h"}
	row := plan.Row{
		Target: "a.md", Lease: "lease-1", IdempotencyKey: "k",
	}
	cases := []struct {
		name string
		aud  applyAuditor
		row  plan.Row
		want string
	}{
		{name: "nil log", aud: applyAuditor{agent: agent, session: sess, planID: "plan-1"}, row: row, want: "nil audit log"},
		{name: "empty target", aud: base, row: plan.Row{Lease: "lease-1"}, want: "empty target"},
		{name: "empty lease", aud: base, row: plan.Row{Target: "a.md"}, want: "lease"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.aud.SignRow(t.Context(), tc.row, "post")
			if err == nil {
				t.Fatal("expected fail-closed error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want substring %q", err, tc.want)
			}
		})
	}
}

func readyApplyAudit(t *testing.T) (store.Store, *audit.Log, store.AgentID, store.SessionID) {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sg := signer.NewMemory()
	log, err := audit.NewLog(st, sg)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.ParseAgentID("a_signrow")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAgent(t.Context(), store.Agent{
		ID: agent, Name: "n", Harness: "h", Status: "ready",
	}); err != nil {
		t.Fatal(err)
	}
	if err := log.EnsureAgentKey(t.Context(), agent, true); err != nil {
		t.Fatal(err)
	}
	sess, err := store.ParseSessionID("s_signrow")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(t.Context(), store.Session{
		ID: sess, AgentID: agent, Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	return st, log, agent, sess
}
