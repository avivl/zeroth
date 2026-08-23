package server_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// The API mappers only run on the way out of a handler, so the way to reach
// their optional-field branches is to put the fields in the store and read the
// endpoint back, not to call the mapper directly.

func TestAgentLeasesMapEveryField(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	ctx := t.Context()
	agentID, err := store.ParseAgentID(server.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	minted := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	seed := []store.Lease{
		{
			ID:      mustParseLease(t, "l1"),
			GrantID: mustParseGrant(t, "g1"),
			ScopeID: mustParseScope(t, "scope-a"),
			AgentID: agentID, ExpiresAt: minted.Add(time.Hour), MintedAt: minted,
		},
		{
			// MintedAt left unset. sqlite's nano() stamps it at write time,
			// so it still comes back set: leaseFrom's omit-when-zero branch
			// is unreachable for anything that went through the store.
			ID:      mustParseLease(t, "l2"),
			GrantID: mustParseGrant(t, "g2"),
			ScopeID: mustParseScope(t, "scope-b"),
			AgentID: agentID, ExpiresAt: minted.Add(2 * time.Hour),
		},
	}
	for _, l := range seed {
		if err := e.st.CreateLease(ctx, l); err != nil {
			t.Fatal(err)
		}
	}

	var list gen.LeaseList
	getInto(t, e.hs.URL+"/agents/"+server.DefaultAgentID+"/leases", &list)
	if len(list.Items) != 2 {
		t.Fatalf("listed %d leases, want 2", len(list.Items))
	}
	byID := map[string]gen.Lease{}
	for _, l := range list.Items {
		byID[string(l.Id)] = l
	}
	l1, ok := byID["l1"]
	if !ok {
		t.Fatalf("l1 missing: %+v", list.Items)
	}
	if string(l1.GrantId) != "g1" || string(l1.ScopeId) != "scope-a" {
		t.Fatalf("l1 refs %+v", l1)
	}
	if l1.MintedAt == nil || !l1.MintedAt.Equal(minted) {
		t.Fatalf("l1 minted_at %+v", l1.MintedAt)
	}
	if !l1.ExpiresAt.Equal(minted.Add(time.Hour)) {
		t.Fatalf("l1 expires_at %s", l1.ExpiresAt)
	}
	if l2 := byID["l2"]; l2.MintedAt == nil {
		t.Fatal("l2 minted_at is nil; the store stamps it even when unset")
	}
}

func TestRunEventsMapTypesAndMessages(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	ctx := t.Context()
	run := createRun(t, e.hs.URL, "event mapping")
	sid, err := store.ParseSessionID(string(run.Id))
	if err != nil {
		t.Fatal(err)
	}

	exam, err := json.Marshal(map[string]string{
		"plan_id": "p1", "verdict": "pass", "notes": "in scope", "reviewer_model": "sonnet",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seed := []store.Event{
		{Type: string(session.EventToken), Payload: "hello", CreatedAt: now},
		{Type: string(session.EventToolCall), Payload: "Read", CreatedAt: now},
		{Type: string(session.EventPlanProposed), Payload: "p1", CreatedAt: now},
		{Type: string(session.EventCrossExam), Payload: string(exam), CreatedAt: now},
		{Type: string(session.EventCheckpointTaken), Payload: "ck1", CreatedAt: now},
		{Type: string(session.EventError), Payload: "boom", CreatedAt: now},
		{Type: string(session.EventBackgrounded), CreatedAt: now},
		{Type: string(session.EventAttached), CreatedAt: now},
		{Type: string(session.EventTerminal), Payload: string(session.PayloadDone), CreatedAt: now},
	}
	if _, err := e.st.AppendEvents(ctx, sid, seed); err != nil {
		t.Fatal(err)
	}

	var page gen.RunEventList
	getInto(t, e.hs.URL+"/runs/"+string(run.Id)+"/events?limit=100", &page)

	types := map[string]int{}
	messages := map[string]bool{}
	for _, ev := range page.Items {
		types[string(ev.Type)]++
		if ev.Message != nil {
			messages[*ev.Message] = true
		}
	}
	for _, want := range []string{"log", "tool_call", "plan_drafted", "cross_exam_verdict", "checkpoint_created", "error", "status_changed"} {
		if types[want] == 0 {
			t.Fatalf("no event mapped to %q: %+v", want, types)
		}
	}
	// A cross-exam verdict renders as "verdict: notes" so the timeline reads
	// without opening the payload.
	if !messages["pass: in scope"] {
		t.Fatalf("cross-exam message missing: %+v", messages)
	}
	for _, want := range []string{"created", "backgrounded", "attached", "completed"} {
		if !messages[want] {
			t.Fatalf("lifecycle message %q missing: %+v", want, messages)
		}
	}
}

func TestRunStatusMapsTerminalStates(t *testing.T) {
	t.Parallel()
	e := examSetup(t)
	ctx := t.Context()

	cases := []struct {
		stored string
		want   gen.RunStatus
	}{
		{string(session.PayloadDone), gen.RunStatusCompleted},
		{string(session.PayloadFailed), gen.RunStatusFailed},
	}
	for _, tc := range cases {
		t.Run(tc.stored, func(t *testing.T) {
			run := createRun(t, e.hs.URL, "status "+tc.stored)
			sid, err := store.ParseSessionID(string(run.Id))
			if err != nil {
				t.Fatal(err)
			}
			sess, err := e.st.GetSession(ctx, sid)
			if err != nil {
				t.Fatal(err)
			}
			sess.FinishedAt = time.Now().UTC()
			sess.PullRequest = "https://github.com/avivl/zeroth/pull/1"
			if err := e.st.UpdateSession(ctx, sess); err != nil {
				t.Fatal(err)
			}
			// Status is projected from the event log, not read off the
			// session row, so the terminal event is what moves it.
			if _, err := e.st.AppendEvents(ctx, sid, []store.Event{
				{Type: string(session.EventTerminal), Payload: tc.stored, CreatedAt: time.Now().UTC()},
			}); err != nil {
				t.Fatal(err)
			}

			var got gen.Run
			getInto(t, e.hs.URL+"/runs/"+string(run.Id), &got)
			if got.Status != tc.want {
				t.Fatalf("status = %s, want %s", got.Status, tc.want)
			}
			if got.FinishedAt == nil {
				t.Fatal("finished_at omitted on a terminal run")
			}
			if got.PullRequest == nil || *got.PullRequest == "" {
				t.Fatalf("pull_request %+v", got.PullRequest)
			}
		})
	}
}

func mustParseLease(t *testing.T, raw string) store.LeaseID {
	t.Helper()
	id, err := store.ParseLeaseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustParseGrant(t *testing.T, raw string) store.GrantID {
	t.Helper()
	id, err := store.ParseGrantID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustParseScope(t *testing.T, raw string) store.ScopeID {
	t.Helper()
	id, err := store.ParseScopeID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
