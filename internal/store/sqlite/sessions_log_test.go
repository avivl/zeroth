package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

func fullSession(t *testing.T, id string, agentID store.AgentID, created time.Time) store.Session {
	t.Helper()
	return store.Session{
		ID:           mustSID(t, id),
		AgentID:      agentID,
		PlanID:       mustPID(t, "p1"),
		Status:       "running",
		Prompt:       "fix the typo",
		TrackerRef:   "42-69",
		Workspace:    store.WorkspaceSource{Repo: "avivl/zeroth", Ref: "main"},
		AutonomyTier: "supervised",
		CreatedAt:    created,
		UpdatedAt:    created,
	}
}

func TestSessionRoundTripKeepsWorkspaceAndTracker(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	agent := fullAgent(t, "a1", created)
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	want := fullSession(t, "s1", agent.ID, created)
	if err := s.CreateSession(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetSession(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.AgentID != want.AgentID || got.PlanID != want.PlanID {
		t.Fatalf("ids %+v", got)
	}
	if got.Status != want.Status || got.Prompt != want.Prompt || got.TrackerRef != want.TrackerRef {
		t.Fatalf("body %+v", got)
	}
	if got.Workspace != want.Workspace || got.AutonomyTier != want.AutonomyTier {
		t.Fatalf("workspace %+v", got)
	}
	if !got.CreatedAt.Equal(created) || !got.UpdatedAt.Equal(created) {
		t.Fatalf("times %+v", got)
	}
	// An unfinished run stores NULL, which must read back as a zero time.
	if !got.FinishedAt.IsZero() || !got.RetractedAt.IsZero() {
		t.Fatalf("terminal times set on a running session: %+v", got)
	}
}

func TestSessionValidationAndMissingRows(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	agent := fullAgent(t, "a1", created)
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	ok := fullSession(t, "s1", agent.ID, created)

	cases := []struct {
		name string
		mut  func(sess *store.Session)
	}{
		{"zero id", func(sess *store.Session) { sess.ID = store.SessionID{} }},
		{"zero agent", func(sess *store.Session) { sess.AgentID = store.AgentID{} }},
		{"empty status", func(sess *store.Session) { sess.Status = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := ok
			tc.mut(&bad)
			if err := s.CreateSession(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("create err = %v, want ErrInvalid", err)
			}
			if err := s.UpdateSession(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("update err = %v, want ErrInvalid", err)
			}
		})
	}

	if _, err := s.GetSession(ctx, store.SessionID{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("get zero id err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetSession(ctx, mustSID(t, "s_absent")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get absent err = %v, want ErrNotFound", err)
	}
	if err := s.UpdateSession(ctx, fullSession(t, "s_absent", agent.ID, created)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update absent err = %v, want ErrNotFound", err)
	}
}

func TestListSessionsFiltersAndPages(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	a1 := fullAgent(t, "a1", base)
	a2 := fullAgent(t, "a2", base)
	for _, a := range []store.Agent{a1, a2} {
		if err := s.CreateAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	seed := []struct {
		id, status string
		agent      store.AgentID
	}{
		{"s1", "running", a1.ID},
		{"s2", "done", a1.ID},
		{"s3", "running", a2.ID},
	}
	for i, sd := range seed {
		sess := fullSession(t, sd.id, sd.agent, base.Add(time.Duration(i)*time.Minute))
		sess.Status = sd.status
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.ListSessions(ctx, store.SessionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 3 || all.Items[0].ID.String() != "s3" {
		t.Fatalf("not newest first: %+v", all.Items)
	}

	filters := []struct {
		name string
		q    store.SessionQuery
		want int
	}{
		{"by status", store.SessionQuery{Status: "running"}, 2},
		{"by agent", store.SessionQuery{AgentID: a1.ID}, 2},
		{"by both", store.SessionQuery{Status: "running", AgentID: a1.ID}, 1},
		{"no match", store.SessionQuery{Status: "nope"}, 0},
	}
	for _, tc := range filters {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			page, err := s.ListSessions(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != tc.want {
				t.Fatalf("listed %d, want %d", len(page.Items), tc.want)
			}
		})
	}

	first, err := s.ListSessions(ctx, store.SessionQuery{PageQuery: store.PageQuery{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Next == "" {
		t.Fatalf("page 1 = %d items, next %q", len(first.Items), first.Next)
	}
	second, err := s.ListSessions(ctx, store.SessionQuery{PageQuery: store.PageQuery{Limit: 2, Cursor: first.Next}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Next != "" {
		t.Fatalf("page 2 = %d items, next %q", len(second.Items), second.Next)
	}
	if _, err := s.ListSessions(ctx, store.SessionQuery{PageQuery: store.PageQuery{Cursor: "not-a-cursor"}}); err == nil {
		t.Fatal("bad cursor accepted")
	}
}

func TestEventLogSeqIsMonotonicAndReplayable(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	agent := fullAgent(t, "a1", created)
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	sess := fullSession(t, "s1", agent.ID, created)
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	// Seq is the order key and the source of truth for a session, so it has
	// to come back strictly increasing and stable across reads.
	batch := []store.Event{
		{Type: "created", Message: "one", CreatedAt: created},
		{Type: "token", Message: "two", Payload: "p2", CreatedAt: created.Add(time.Second)},
		{Type: "terminal", Message: "three", PlanID: mustPID(t, "p1"), CreatedAt: created.Add(2 * time.Second)},
	}
	got, err := s.AppendEvents(ctx, sess.ID, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("appended %d, want 3", len(got))
	}
	for i, ev := range got {
		if ev.SessionID != sess.ID || ev.Type != batch[i].Type || ev.Message != batch[i].Message {
			t.Fatalf("event %d = %+v", i, ev)
		}
		if i > 0 && ev.Seq <= got[i-1].Seq {
			t.Fatalf("seq not increasing: %d then %d", got[i-1].Seq, ev.Seq)
		}
		if ev.ID.IsZero() {
			t.Fatalf("event %d has no id", i)
		}
	}

	single, err := s.AppendEvent(ctx, sess.ID, store.Event{Type: "token", Message: "four"})
	if err != nil {
		t.Fatal(err)
	}
	if single.Seq <= got[2].Seq {
		t.Fatalf("AppendEvent seq %d, want above %d", single.Seq, got[2].Seq)
	}
	if single.CreatedAt.IsZero() {
		t.Fatal("AppendEvent left created_at unset")
	}

	last, err := s.ReplayLast(ctx, sess.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(last) != 2 {
		t.Fatalf("replay returned %d, want 2", len(last))
	}
	// Replay hands back the tail in forward order so a projection can fold it.
	if last[0].Message != "three" || last[1].Message != "four" {
		t.Fatalf("replay order %+v", last)
	}

	after, err := s.EventsAfter(ctx, sess.ID, got[0].Seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 3 {
		t.Fatalf("events after first = %d, want 3", len(after))
	}
	if after[0].Seq != got[1].Seq {
		t.Fatalf("events after skipped to %d, want %d", after[0].Seq, got[1].Seq)
	}
	none, err := s.EventsAfter(ctx, sess.ID, single.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("events after the tail = %d, want 0", len(none))
	}
}

func TestEventLogValidation(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	created := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	agent := fullAgent(t, "a1", created)
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	sess := fullSession(t, "s1", agent.ID, created)
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	if _, err := s.AppendEvents(ctx, store.SessionID{}, []store.Event{{Type: "token"}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("zero session err = %v, want ErrInvalid", err)
	}
	if _, err := s.AppendEvents(ctx, sess.ID, nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("empty batch err = %v, want ErrInvalid", err)
	}
	if _, err := s.AppendEvents(ctx, sess.ID, []store.Event{{Type: ""}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("untyped event err = %v, want ErrInvalid", err)
	}

	// A batch that fails partway must roll back whole: the log is the source
	// of truth, so a half-written batch would be a fabricated history.
	if _, err := s.AppendEvents(ctx, sess.ID, []store.Event{{Type: "ok"}, {Type: ""}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("partial batch err = %v, want ErrInvalid", err)
	}
	all, err := s.EventsAfter(ctx, sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("rolled-back batch left %d events behind", len(all))
	}

	if _, err := s.ReplayLast(ctx, store.SessionID{}, 1); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("replay zero session err = %v, want ErrInvalid", err)
	}
	if _, err := s.ReplayLast(ctx, sess.ID, 0); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("replay n=0 err = %v, want ErrInvalid", err)
	}
	if _, err := s.EventsAfter(ctx, store.SessionID{}, 0); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("events after zero session err = %v, want ErrInvalid", err)
	}
}
