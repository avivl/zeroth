package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

func TestDeleteLeaseRemovesRow(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	agent := store.Agent{ID: mustAID(t, "lease-agent"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	lid, err := store.ParseLeaseID("lease-1")
	if err != nil {
		t.Fatal(err)
	}
	gid, err := store.ParseGrantID("grant-1")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := store.ParseScopeID("scope-1")
	if err != nil {
		t.Fatal(err)
	}
	l := store.Lease{
		ID:        lid,
		GrantID:   gid,
		ScopeID:   sid,
		AgentID:   agent.ID,
		ExpiresAt: time.Unix(10, 0).UTC(),
		MintedAt:  time.Unix(1, 0).UTC(),
	}
	if err := s.CreateLease(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLease(ctx, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLease(ctx, l.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetLease after delete: %v", err)
	}
	if err := s.DeleteLease(ctx, l.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteLease missing: %v", err)
	}
	if err := s.DeleteLease(ctx, store.LeaseID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("DeleteLease empty: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLease(ctx, l.ID); !errors.Is(err, store.ErrClosed) {
		t.Fatalf("DeleteLease closed: %v", err)
	}
}
