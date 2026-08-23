package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

func auditRecord(t *testing.T, id string, created time.Time) store.AuditRecord {
	t.Helper()
	return store.AuditRecord{
		ID:            mustAuID(t, id),
		Action:        "plan.approve",
		Target:        "p1",
		PlanHash:      "h1",
		Precondition:  "pre",
		Postcondition: "post",
		LeaseID:       mustLeaseID(t, "l1"),
		Approver:      "operator",
		AgentPubKey:   "pk-1",
		PrevHash:      "",
		Hash:          "hash-" + id,
		Signature:     "sig-" + id,
		AgentID:       mustAID(t, "a1"),
		SessionID:     mustSID(t, "s1"),
		ResourceType:  "plan",
		ResourceID:    "p1",
		Actor:         "operator",
		CreatedAt:     created,
	}
}

func TestAppendAuditRoundTripKeepsSignedFields(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	planFixture(t, s)
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	want := auditRecord(t, "au1", created)

	returned, err := s.AppendAudit(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	if returned.ID != want.ID || returned.Hash != want.Hash {
		t.Fatalf("returned %+v", returned)
	}

	got, err := s.GetAudit(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Every field here is part of what the chain signs, so a column that
	// silently drops on the way back would break offline verification.
	if got.Action != want.Action || got.Target != want.Target || got.PlanHash != want.PlanHash {
		t.Fatalf("action fields %+v", got)
	}
	if got.Precondition != want.Precondition || got.Postcondition != want.Postcondition {
		t.Fatalf("pre/post %+v", got)
	}
	if got.LeaseID != want.LeaseID || got.Approver != want.Approver || got.AgentPubKey != want.AgentPubKey {
		t.Fatalf("approver fields %+v", got)
	}
	if got.PrevHash != want.PrevHash || got.Hash != want.Hash || got.Signature != want.Signature {
		t.Fatalf("chain fields %+v", got)
	}
	if got.AgentID != want.AgentID || got.SessionID != want.SessionID {
		t.Fatalf("refs %+v", got)
	}
	if got.ResourceType != want.ResourceType || got.ResourceID != want.ResourceID || got.Actor != want.Actor {
		t.Fatalf("resource %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("created_at %s want %s", got.CreatedAt, created)
	}
}

func TestAppendAuditFillsTargetActorAndTimestamp(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	planFixture(t, s)

	r := auditRecord(t, "au1", time.Time{})
	r.Target = ""
	r.Actor = ""
	before := time.Now().UTC()
	got, err := s.AppendAudit(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Target != r.ResourceID {
		t.Fatalf("target = %q, want it to default to resource id %q", got.Target, r.ResourceID)
	}
	if got.Actor != r.Approver {
		t.Fatalf("actor = %q, want it to default to approver %q", got.Actor, r.Approver)
	}
	if got.CreatedAt.Before(before) {
		t.Fatalf("created_at = %s, want it stamped at append time", got.CreatedAt)
	}
}

func TestAppendAuditValidation(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	planFixture(t, s)
	ok := auditRecord(t, "au1", time.Unix(0, 1_700_000_000_000_000_000).UTC())

	cases := []struct {
		name string
		mut  func(r *store.AuditRecord)
	}{
		{"zero id", func(r *store.AuditRecord) { r.ID = store.AuditID{} }},
		{"empty action", func(r *store.AuditRecord) { r.Action = "" }},
		{"empty resource type", func(r *store.AuditRecord) { r.ResourceType = "" }},
		{"empty resource id", func(r *store.AuditRecord) { r.ResourceID = "" }},
		{"empty signature", func(r *store.AuditRecord) { r.Signature = "" }},
		{"empty hash", func(r *store.AuditRecord) { r.Hash = "" }},
		{"empty pubkey", func(r *store.AuditRecord) { r.AgentPubKey = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := ok
			tc.mut(&bad)
			if _, err := s.AppendAudit(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("append err = %v, want ErrInvalid", err)
			}
		})
	}

	if _, err := s.GetAudit(ctx, store.AuditID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("get zero id err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetAudit(ctx, mustAuID(t, "au_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get absent err = %v, want ErrNotFound", err)
	}
}

func TestListAuditFiltersAndAuditChainIsOldestFirst(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	planFixture(t, s)
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	prev := ""
	for i, id := range []string{"au1", "au2", "au3"} {
		r := auditRecord(t, id, base.Add(time.Duration(i)*time.Minute))
		r.PrevHash = prev
		if i == 2 {
			r.ResourceType = "run"
			r.ResourceID = "s1"
		}
		if _, err := s.AppendAudit(ctx, r); err != nil {
			t.Fatal(err)
		}
		prev = r.Hash
	}

	all, err := s.ListAudit(ctx, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 3 || all.Items[0].ID.String() != "au3" {
		t.Fatalf("list not newest first: %+v", all.Items)
	}

	byType, err := s.ListAudit(ctx, store.AuditQuery{ResourceType: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byType.Items) != 2 {
		t.Fatalf("resource type filter listed %d, want 2", len(byType.Items))
	}
	byID, err := s.ListAudit(ctx, store.AuditQuery{ResourceID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byID.Items) != 1 {
		t.Fatalf("resource id filter listed %d, want 1", len(byID.Items))
	}
	bySession, err := s.ListAudit(ctx, store.AuditQuery{SessionID: mustSID(t, "s1")})
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession.Items) != 3 {
		t.Fatalf("session filter listed %d, want 3", len(bySession.Items))
	}

	first, err := s.ListAudit(ctx, store.AuditQuery{PageQuery: store.PageQuery{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Next == "" {
		t.Fatalf("page 1 = %d items, next %q", len(first.Items), first.Next)
	}
	second, err := s.ListAudit(ctx, store.AuditQuery{PageQuery: store.PageQuery{Limit: 2, Cursor: first.Next}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Next != "" {
		t.Fatalf("page 2 = %d items, next %q", len(second.Items), second.Next)
	}
	if _, err := s.ListAudit(ctx, store.AuditQuery{PageQuery: store.PageQuery{Cursor: "not-a-cursor"}}); err == nil {
		t.Fatal("bad cursor accepted")
	}

	// Verify walks the chain oldest first, and each PrevHash has to match the
	// row before it or the trail is not verifiable offline.
	chain, err := s.AuditChain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 3 {
		t.Fatalf("chain length %d, want 3", len(chain))
	}
	if chain[0].ID.String() != "au1" || chain[2].ID.String() != "au3" {
		t.Fatalf("chain not oldest first: %s..%s", chain[0].ID, chain[2].ID)
	}
	for i := 1; i < len(chain); i++ {
		if chain[i].PrevHash != chain[i-1].Hash {
			t.Fatalf("chain break at %d: prev %q, want %q", i, chain[i].PrevHash, chain[i-1].Hash)
		}
	}
}

func TestAgentKeysAreAppendOnlyAndOrdered(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	agent := fullAgent(t, "a1", created)
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}

	// Rotation inserts, it does not replace: signatures made under the old
	// key still have to verify.
	for i, pk := range []string{"pk-1", "pk-2"} {
		k := store.AgentKey{AgentID: agent.ID, PubKey: pk, CreatedAt: created.Add(time.Duration(i) * time.Minute)}
		if err := s.AppendAgentKey(ctx, k); err != nil {
			t.Fatal(err)
		}
	}

	keys, err := s.ListAgentKeys(ctx, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys %+v, want 2", keys)
	}
	if keys[0].PubKey != "pk-1" || keys[1].PubKey != "pk-2" {
		t.Fatalf("keys not oldest first: %+v", keys)
	}
	if keys[0].AgentID != agent.ID || !keys[0].CreatedAt.Equal(created) {
		t.Fatalf("key fields %+v", keys[0])
	}

	none, err := s.ListAgentKeys(ctx, mustAID(t, "a_absent"))
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("unknown agent returned %d keys", len(none))
	}

	bad := []struct {
		name string
		key  store.AgentKey
	}{
		{"zero agent", store.AgentKey{PubKey: "pk"}},
		{"empty pubkey", store.AgentKey{AgentID: agent.ID}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := s.AppendAgentKey(ctx, tc.key); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCheckpointRoundTripAndList(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	want := store.Checkpoint{
		ID:        mustCkID(t, "ck1"),
		SessionID: sid,
		Label:     "pre-apply",
		Location:  "/var/lib/zeroth/checkpoints/ck1.tar",
		CreatedAt: base,
	}
	if err := s.CreateCheckpoint(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCheckpoint(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.SessionID != want.SessionID {
		t.Fatalf("identity %+v", got)
	}
	if got.Label != want.Label || got.Location != want.Location {
		t.Fatalf("body %+v", got)
	}
	if !got.CreatedAt.Equal(base) {
		t.Fatalf("created_at %s want %s", got.CreatedAt, base)
	}

	for i, id := range []string{"ck2", "ck3"} {
		c := store.Checkpoint{
			ID: mustCkID(t, id), SessionID: sid, Label: id,
			Location: "/tmp/" + id, CreatedAt: base.Add(time.Duration(i+1) * time.Minute),
		}
		if err := s.CreateCheckpoint(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.ListCheckpoints(ctx, store.CheckpointQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 3 || all.Items[0].ID.String() != "ck3" {
		t.Fatalf("not newest first: %+v", all.Items)
	}
	bySession, err := s.ListCheckpoints(ctx, store.CheckpointQuery{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession.Items) != 3 {
		t.Fatalf("session filter listed %d, want 3", len(bySession.Items))
	}
	none, err := s.ListCheckpoints(ctx, store.CheckpointQuery{SessionID: mustSID(t, "s_absent")})
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Items) != 0 {
		t.Fatalf("unknown session listed %d", len(none.Items))
	}

	first, err := s.ListCheckpoints(ctx, store.CheckpointQuery{PageQuery: store.PageQuery{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Next == "" {
		t.Fatalf("page 1 = %d items, next %q", len(first.Items), first.Next)
	}
	second, err := s.ListCheckpoints(ctx, store.CheckpointQuery{PageQuery: store.PageQuery{Limit: 2, Cursor: first.Next}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Next != "" {
		t.Fatalf("page 2 = %d items, next %q", len(second.Items), second.Next)
	}
	if _, err := s.ListCheckpoints(ctx, store.CheckpointQuery{PageQuery: store.PageQuery{Cursor: "not-a-cursor"}}); err == nil {
		t.Fatal("bad cursor accepted")
	}

	if err := s.CreateCheckpoint(ctx, store.Checkpoint{SessionID: sid}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("zero id err = %v, want ErrInvalid", err)
	}
	if err := s.CreateCheckpoint(ctx, store.Checkpoint{ID: mustCkID(t, "ck9")}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("zero session err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetCheckpoint(ctx, mustCkID(t, "ck_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get absent err = %v, want ErrNotFound", err)
	}
}

func mustCkID(t testing.TB, raw string) store.CheckpointID {
	t.Helper()
	id, err := store.ParseCheckpointID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
