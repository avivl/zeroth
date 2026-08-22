package sqlite

import (
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

func TestSessionRetractFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	agent := store.Agent{ID: mustAID(t, "a1"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	sess := store.Session{
		ID:            mustSID(t, "s1"),
		AgentID:       agent.ID,
		Status:        "done",
		PullRequest:   "https://github.com/avivl/zeroth/pull/48",
		RetractReason: "Apply overwrote README.md instead of patching it.",
		CreatedAt:     now,
		UpdatedAt:     now,
		FinishedAt:    now,
		RetractedAt:   now,
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PullRequest != sess.PullRequest || got.RetractReason != sess.RetractReason || got.RetractedAt.IsZero() {
		t.Fatalf("get %+v", got)
	}
	if !got.RetractedAt.Equal(sess.RetractedAt) {
		t.Fatalf("retracted_at %s want %s", got.RetractedAt, sess.RetractedAt)
	}

	sess.RetractReason = "post-apply hash mismatch"
	sess.UpdatedAt = now.Add(time.Second)
	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListSessions(ctx, store.SessionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RetractReason != sess.RetractReason || page.Items[0].PullRequest != sess.PullRequest {
		t.Fatalf("list %+v", page.Items)
	}
}
