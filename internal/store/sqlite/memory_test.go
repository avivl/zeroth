package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

func mustMID(t testing.TB, raw string) store.MemoryID {
	t.Helper()
	id, err := store.ParseMemoryID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustMPID(t testing.TB, raw string) store.MemoryProposalID {
	t.Helper()
	id, err := store.ParseMemoryProposalID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fullMemory(t *testing.T, id string, created time.Time) store.MemoryEntry {
	t.Helper()
	return store.MemoryEntry{
		ID:         mustMID(t, id),
		Kind:       "operator",
		RefID:      "ref-1",
		Key:        "deploy-window",
		Content:    "deploys land Tuesdays",
		Author:     "aviv",
		AuthorKind: "human",
		Source:     "chat",
		Action:     "create",
		Version:    2,
		CreatedAt:  created,
		UpdatedAt:  created.Add(time.Minute),
		History: []store.MemoryRevision{{
			Version:    1,
			Key:        "deploy-window",
			Body:       "deploys land Mondays",
			Author:     "aviv",
			AuthorKind: "human",
			Action:     "create",
			Source:     "chat",
			At:         created,
		}},
	}
}

func TestMemoryRoundTripKeepsHistoryAndTombstone(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	want := fullMemory(t, "m1", created)
	if err := s.CreateMemory(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetMemory(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Kind != want.Kind || got.RefID != want.RefID || got.Key != want.Key {
		t.Fatalf("identity %+v", got)
	}
	if got.Content != want.Content || got.Author != want.Author || got.AuthorKind != want.AuthorKind {
		t.Fatalf("body %+v", got)
	}
	if got.Source != want.Source || got.Action != want.Action || got.Version != 2 || got.Deleted {
		t.Fatalf("provenance %+v", got)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times %+v", got)
	}
	if len(got.History) != 1 || got.History[0] != want.History[0] {
		t.Fatalf("history %+v", got.History)
	}

	// The tombstone is a column, not a delete: the row and its history stay
	// readable so an operator can see what was retracted.
	want.Deleted = true
	want.Action = "delete"
	want.Version = 3
	want.UpdatedAt = created.Add(2 * time.Minute)
	if err := s.UpdateMemory(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetMemory(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Deleted || got.Action != "delete" || got.Version != 3 {
		t.Fatalf("tombstone %+v", got)
	}
	if len(got.History) != 1 {
		t.Fatalf("history lost on tombstone: %+v", got.History)
	}
}

func TestCreateMemoryDefaultsVersionAndUpdatedAt(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	m := store.MemoryEntry{
		ID:        mustMID(t, "m1"),
		Kind:      "session",
		Content:   "first fact",
		CreatedAt: created,
	}
	if err := s.CreateMemory(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMemory(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Fatalf("version = %d, want the 1 default", got.Version)
	}
	if !got.UpdatedAt.Equal(created) {
		t.Fatalf("updated_at = %s, want it to fall back to created_at %s", got.UpdatedAt, created)
	}
	if len(got.History) != 0 {
		t.Fatalf("history = %+v, want empty", got.History)
	}
}

func TestMemoryWriteValidationAndMissingRows(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	ok := fullMemory(t, "m1", created)

	cases := []struct {
		name string
		mut  func(m *store.MemoryEntry)
	}{
		{"zero id", func(m *store.MemoryEntry) { m.ID = store.MemoryID{} }},
		{"empty kind", func(m *store.MemoryEntry) { m.Kind = "" }},
		{"empty content", func(m *store.MemoryEntry) { m.Content = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := ok
			tc.mut(&bad)
			if err := s.CreateMemory(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("create err = %v, want ErrInvalid", err)
			}
			if err := s.UpdateMemory(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("update err = %v, want ErrInvalid", err)
			}
		})
	}

	if _, err := s.GetMemory(ctx, store.MemoryID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("get zero id err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetMemory(ctx, mustMID(t, "m_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get absent err = %v, want ErrNotFound", err)
	}
	if err := s.UpdateMemory(ctx, fullMemory(t, "m_absent", created)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update absent err = %v, want ErrNotFound", err)
	}
}

func TestListMemoryFiltersAndPages(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	seed := []struct {
		id, kind, ref, key string
	}{
		{"m1", "operator", "ref-1", "k1"},
		{"m2", "operator", "ref-1", "k2"},
		{"m3", "session", "ref-2", "k1"},
		{"m4", "agent", "ref-1", "k3"},
	}
	for i, sd := range seed {
		m := store.MemoryEntry{
			ID: mustMID(t, sd.id), Kind: sd.kind, RefID: sd.ref, Key: sd.key,
			Content: "body " + sd.id, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := s.CreateMemory(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.ListMemory(ctx, store.MemoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 4 || all.Items[0].ID.String() != "m4" || all.Items[3].ID.String() != "m1" {
		t.Fatalf("not newest first: %+v", all.Items)
	}

	filters := []struct {
		name string
		q    store.MemoryQuery
		want int
	}{
		{"by kind", store.MemoryQuery{Kind: "operator"}, 2},
		{"by ref", store.MemoryQuery{RefID: "ref-1"}, 3},
		{"by key", store.MemoryQuery{Key: "k1"}, 2},
		{"kind and ref", store.MemoryQuery{Kind: "operator", RefID: "ref-1"}, 2},
		{"all three", store.MemoryQuery{Kind: "operator", RefID: "ref-1", Key: "k2"}, 1},
		{"no match", store.MemoryQuery{Kind: "nope"}, 0},
	}
	for _, tc := range filters {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			page, err := s.ListMemory(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != tc.want {
				t.Fatalf("listed %d, want %d", len(page.Items), tc.want)
			}
		})
	}

	first, err := s.ListMemory(ctx, store.MemoryQuery{PageQuery: store.PageQuery{Limit: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 3 || first.Next == "" {
		t.Fatalf("page 1 = %d items, next %q", len(first.Items), first.Next)
	}
	second, err := s.ListMemory(ctx, store.MemoryQuery{PageQuery: store.PageQuery{Limit: 3, Cursor: first.Next}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Next != "" {
		t.Fatalf("page 2 = %d items, next %q", len(second.Items), second.Next)
	}
	if second.Items[0].ID.String() != "m1" {
		t.Fatalf("cursor landed on %s, want m1", second.Items[0].ID)
	}

	if _, err := s.ListMemory(ctx, store.MemoryQuery{PageQuery: store.PageQuery{Cursor: "not-a-cursor"}}); err == nil {
		t.Fatal("bad cursor accepted")
	}
}

func TestMemoryProposalRoundTripAndReview(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	sid := planFixture(t, s)
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	want := store.MemoryProposal{
		ID:         mustMPID(t, "mp1"),
		Kind:       "session",
		RefID:      "ref-1",
		SessionID:  sid,
		Key:        "deploy-window",
		Content:    "deploys land Tuesdays",
		Author:     "claudecode",
		AuthorKind: "agent",
		Source:     "harness",
		Status:     "pending",
		CreatedAt:  created,
	}
	if err := s.CreateMemoryProposal(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMemoryProposal(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Kind != want.Kind || got.RefID != want.RefID || got.SessionID != want.SessionID {
		t.Fatalf("identity %+v", got)
	}
	if got.Key != want.Key || got.Content != want.Content || got.Status != want.Status {
		t.Fatalf("body %+v", got)
	}
	if got.Author != want.Author || got.AuthorKind != want.AuthorKind || got.Source != want.Source {
		t.Fatalf("provenance %+v", got)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("created_at %s want %s", got.CreatedAt, want.CreatedAt)
	}
	// An unreviewed proposal stores NULL, which has to read back as a zero
	// time rather than the epoch.
	if !got.ReviewedAt.IsZero() {
		t.Fatalf("reviewed_at = %s, want zero for an unreviewed proposal", got.ReviewedAt)
	}

	want.Status = "accepted"
	want.MemoryID = mustMID(t, "m1")
	want.ReviewedAt = created.Add(time.Hour)
	if err := s.UpdateMemoryProposal(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetMemoryProposal(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "accepted" || got.MemoryID != want.MemoryID {
		t.Fatalf("after review %+v", got)
	}
	if !got.ReviewedAt.Equal(want.ReviewedAt) {
		t.Fatalf("reviewed_at %s want %s", got.ReviewedAt, want.ReviewedAt)
	}
}

func TestMemoryProposalValidationAndMissingRows(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	ok := store.MemoryProposal{
		ID: mustMPID(t, "mp1"), Kind: "session", Content: "body",
		Status: "pending", CreatedAt: created,
	}

	cases := []struct {
		name string
		mut  func(p *store.MemoryProposal)
	}{
		{"zero id", func(p *store.MemoryProposal) { p.ID = store.MemoryProposalID{} }},
		{"empty kind", func(p *store.MemoryProposal) { p.Kind = "" }},
		{"empty content", func(p *store.MemoryProposal) { p.Content = "" }},
		{"empty status", func(p *store.MemoryProposal) { p.Status = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := ok
			tc.mut(&bad)
			if err := s.CreateMemoryProposal(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("create err = %v, want ErrInvalid", err)
			}
			if err := s.UpdateMemoryProposal(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("update err = %v, want ErrInvalid", err)
			}
		})
	}

	if _, err := s.GetMemoryProposal(ctx, store.MemoryProposalID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("get zero id err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetMemoryProposal(ctx, mustMPID(t, "mp_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get absent err = %v, want ErrNotFound", err)
	}
	absent := ok
	absent.ID = mustMPID(t, "mp_absent")
	if err := s.UpdateMemoryProposal(ctx, absent); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update absent err = %v, want ErrNotFound", err)
	}
}

func TestListMemoryProposalsFiltersAndPages(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	for i, sd := range []struct{ id, status string }{
		{"mp1", "pending"}, {"mp2", "pending"}, {"mp3", "accepted"}, {"mp4", "rejected"},
	} {
		p := store.MemoryProposal{
			ID: mustMPID(t, sd.id), Kind: "session", Content: "body " + sd.id,
			Status: sd.status, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := s.CreateMemoryProposal(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.ListMemoryProposals(ctx, store.MemoryProposalQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 4 || all.Items[0].ID.String() != "mp4" {
		t.Fatalf("not newest first: %+v", all.Items)
	}

	pending, err := s.ListMemoryProposals(ctx, store.MemoryProposalQuery{Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Items) != 2 {
		t.Fatalf("pending listed %d, want 2", len(pending.Items))
	}

	first, err := s.ListMemoryProposals(ctx, store.MemoryProposalQuery{PageQuery: store.PageQuery{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Next == "" {
		t.Fatalf("page 1 = %d items, next %q", len(first.Items), first.Next)
	}
	second, err := s.ListMemoryProposals(ctx, store.MemoryProposalQuery{PageQuery: store.PageQuery{Limit: 2, Cursor: first.Next}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 2 || second.Next != "" {
		t.Fatalf("page 2 = %d items, next %q", len(second.Items), second.Next)
	}
	if second.Items[0].ID.String() != "mp2" {
		t.Fatalf("cursor landed on %s, want mp2", second.Items[0].ID)
	}

	if _, err := s.ListMemoryProposals(ctx, store.MemoryProposalQuery{PageQuery: store.PageQuery{Cursor: "not-a-cursor"}}); err == nil {
		t.Fatal("bad cursor accepted")
	}
}
