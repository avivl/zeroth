package session_test

import (
	"errors"
	"testing"

	"github.com/avivl/zeroth/internal/session"
)

type transCase struct {
	from     session.Status
	attach   session.Attachment
	typ      session.EventType
	payload  string
	to       session.Status
	toAtt    session.Attachment
	contract session.CompletionContract
}

func legalTransitions() []transCase {
	def := session.DefaultCompletionContract()
	var out []transCase
	for _, att := range []session.Attachment{session.AttachmentAttached, session.AttachmentBackground} {
		out = append(out,
			transCase{from: session.StatusPending, attach: att, typ: session.EventStarted, to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusPending, attach: att, typ: session.EventError, payload: "boom", to: session.StatusPending, toAtt: att},
			transCase{from: session.StatusPending, attach: att, typ: session.EventCheckpointTaken, payload: "ckpt", to: session.StatusPending, toAtt: att},
			transCase{from: session.StatusPending, attach: att, typ: session.EventTerminal, payload: session.PayloadFailed, to: session.StatusFailed, toAtt: att},

			transCase{from: session.StatusRunning, attach: att, typ: session.EventToken, payload: "hi", to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventToolCall, payload: "bash", to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventPlanProposed, payload: "plan-1", to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventCrossExam, payload: "fail", to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventApprovalRequested, payload: "plan-1", to: session.StatusAwaitingApproval, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventCheckpointTaken, payload: "ckpt", to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventError, payload: "boom", to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventSteered, payload: "nudge", to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventTerminal, payload: session.PayloadDone, to: session.StatusDone, toAtt: att},
			transCase{from: session.StatusRunning, attach: att, typ: session.EventTerminal, payload: session.PayloadFailed, to: session.StatusFailed, toAtt: att},

			transCase{from: session.StatusAwaitingApproval, attach: att, typ: session.EventApplying, to: session.StatusApplying, toAtt: att},
			transCase{from: session.StatusAwaitingApproval, attach: att, typ: session.EventChangesRequested, to: session.StatusRunning, toAtt: att},
			transCase{from: session.StatusAwaitingApproval, attach: att, typ: session.EventCheckpointTaken, payload: "ckpt", to: session.StatusAwaitingApproval, toAtt: att},
			transCase{from: session.StatusAwaitingApproval, attach: att, typ: session.EventError, payload: "boom", to: session.StatusAwaitingApproval, toAtt: att},
			transCase{from: session.StatusAwaitingApproval, attach: att, typ: session.EventSteered, payload: "nudge", to: session.StatusAwaitingApproval, toAtt: att},
			transCase{from: session.StatusAwaitingApproval, attach: att, typ: session.EventTerminal, payload: session.PayloadFailed, to: session.StatusFailed, toAtt: att},

			transCase{from: session.StatusApplying, attach: att, typ: session.EventToken, payload: "hi", to: session.StatusApplying, toAtt: att},
			transCase{from: session.StatusApplying, attach: att, typ: session.EventToolCall, payload: "bash", to: session.StatusApplying, toAtt: att},
			transCase{from: session.StatusApplying, attach: att, typ: session.EventCheckpointTaken, payload: "ckpt", to: session.StatusApplying, toAtt: att},
			transCase{from: session.StatusApplying, attach: att, typ: session.EventError, payload: "boom", to: session.StatusApplying, toAtt: att},
			transCase{from: session.StatusApplying, attach: att, typ: session.EventSteered, payload: "nudge", to: session.StatusApplying, toAtt: att},
			transCase{from: session.StatusApplying, attach: att, typ: session.EventTerminal, payload: session.PayloadDone, to: session.StatusDone, toAtt: att},
			transCase{from: session.StatusApplying, attach: att, typ: session.EventTerminal, payload: session.PayloadFailed, to: session.StatusFailed, toAtt: att},
		)
	}
	for _, st := range []session.Status{
		session.StatusPending, session.StatusRunning,
		session.StatusAwaitingApproval, session.StatusApplying,
	} {
		out = append(out,
			transCase{from: st, attach: session.AttachmentAttached, typ: session.EventBackgrounded, to: st, toAtt: session.AttachmentBackground, contract: def},
			transCase{from: st, attach: session.AttachmentBackground, typ: session.EventAttached, to: st, toAtt: session.AttachmentAttached},
		)
	}
	return out
}

type transKey struct {
	from    session.Status
	attach  session.Attachment
	typ     session.EventType
	payload string
}

func TestEveryTransition(t *testing.T) {
	t.Parallel()
	id := mustID(t, "sess-table")
	allowed := map[transKey]transCase{}
	for _, tc := range legalTransitions() {
		allowed[transKey{tc.from, tc.attach, tc.typ, tc.payload}] = tc
	}

	statuses := []session.Status{
		session.StatusPending, session.StatusRunning,
		session.StatusAwaitingApproval, session.StatusApplying,
		session.StatusDone, session.StatusFailed,
	}
	attachments := []session.Attachment{session.AttachmentAttached, session.AttachmentBackground}
	types := []session.EventType{
		session.EventCreated,
		session.EventStarted,
		session.EventToken,
		session.EventToolCall,
		session.EventPlanProposed,
		session.EventCrossExam,
		session.EventApprovalRequested,
		session.EventChangesRequested,
		session.EventApplying,
		session.EventCheckpointTaken,
		session.EventError,
		session.EventSteered,
		session.EventBackgrounded,
		session.EventAttached,
		session.EventTerminal,
	}

	payloads := func(typ session.EventType) []string {
		switch typ {
		case session.EventTerminal:
			return []string{session.PayloadDone, session.PayloadFailed}
		case session.EventToken:
			return []string{"hi"}
		case session.EventToolCall:
			return []string{"bash"}
		case session.EventPlanProposed, session.EventApprovalRequested:
			return []string{"plan-1"}
		case session.EventCrossExam:
			return []string{"fail"}
		case session.EventCheckpointTaken:
			return []string{"ckpt"}
		case session.EventError:
			return []string{"boom"}
		case session.EventSteered:
			return []string{"nudge"}
		default:
			return []string{""}
		}
	}

	t.Run("created from empty", func(t *testing.T) {
		t.Parallel()
		got, err := session.Apply(session.State{}, session.Event{SessionID: id, Type: session.EventCreated})
		if err != nil {
			t.Fatal(err)
		}
		want := session.State{ID: id, Status: session.StatusPending, Attachment: session.AttachmentAttached}
		if got != want {
			t.Fatalf("got %+v want %+v", got, want)
		}
	})

	t.Run("created from empty with zero id", func(t *testing.T) {
		t.Parallel()
		if _, err := session.Apply(session.State{}, session.Event{Type: session.EventCreated}); err == nil {
			t.Fatal("expected error")
		}
	})

	for _, from := range statuses {
		for _, att := range attachments {
			for _, typ := range types {
				for _, payload := range payloads(typ) {
					from, att, typ, payload := from, att, typ, payload
					name := string(from) + "/" + string(att) + "/" + string(typ) + "/" + payload
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						st := session.State{ID: id, Status: from, Attachment: att}
						ev := session.Event{SessionID: id, Type: typ, Payload: payload}
						got, err := session.Apply(st, ev)
						tc, ok := allowed[transKey{from, att, typ, payload}]
						if !ok {
							if err == nil {
								t.Fatalf("wanted illegal, got %+v", got)
							}
							if typ == session.EventCreated || from.Terminal() || !payloadOK(typ, payload) {
								return
							}
							if !errors.Is(err, session.ErrIllegalTransition) {
								t.Fatalf("illegal want ErrIllegalTransition, got %v", err)
							}
							return
						}
						if err != nil {
							t.Fatalf("legal transition: %v", err)
						}
						want := session.State{
							ID:         id,
							Status:     tc.to,
							Attachment: tc.toAtt,
							Contract:   tc.contract,
						}
						if got != want {
							t.Fatalf("got %+v want %+v", got, want)
						}
					})
				}
			}
		}
	}
}

func payloadOK(typ session.EventType, payload string) bool {
	switch typ {
	case session.EventTerminal:
		return payload == session.PayloadDone || payload == session.PayloadFailed
	case session.EventSteered, session.EventToolCall, session.EventError:
		return payload != ""
	default:
		return true
	}
}

func TestUnknownEventType(t *testing.T) {
	t.Parallel()
	id := mustID(t, "sess-unknown")
	st := session.State{ID: id, Status: session.StatusRunning, Attachment: session.AttachmentAttached}
	if _, err := session.Apply(st, session.Event{SessionID: id, Type: "nope"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestIDMismatch(t *testing.T) {
	t.Parallel()
	a := mustID(t, "sess-a")
	b := mustID(t, "sess-b")
	st := session.State{ID: a, Status: session.StatusRunning, Attachment: session.AttachmentAttached}
	if _, err := session.Apply(st, session.Event{SessionID: b, Type: session.EventToken, Payload: "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestIllegalDoesNotAppend(t *testing.T) {
	t.Parallel()
	m, _ := mustNew(t, "sess-no-append")
	ctx := t.Context()
	before, err := m.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Succeed(ctx); err == nil {
		t.Fatal("succeed from pending: expected error")
	}
	if err := m.Steer(ctx, "nope"); err == nil {
		t.Fatal("steer from pending: expected error")
	}
	if err := m.Foreground(ctx); err == nil {
		t.Fatal("foreground from attached: expected error")
	}
	after, err := m.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("illegal transition appended: before %d after %d", len(before), len(after))
	}
}

func TestReplayMatchesWalk(t *testing.T) {
	t.Parallel()
	m, _ := mustNew(t, "sess-replay")
	ctx := t.Context()
	steps := []func() error{
		func() error { return m.Start(ctx) },
		func() error { return m.EmitToken(ctx, "tok") },
		func() error { return m.EmitToolCall(ctx, "bash") },
		func() error { return m.ProposePlan(ctx, "plan-1") },
		func() error { return m.RecordCrossExam(ctx, "pass") },
		func() error { return m.RequestApproval(ctx, "plan-1") },
		func() error { return m.RequestChanges(ctx, "narrow the diff") },
		func() error { return m.Background(ctx, nil) },
		func() error { return m.RequestApproval(ctx, "plan-2") },
		func() error { return m.BeginApply(ctx) },
		func() error { return m.TakeCheckpoint(ctx, "ckpt") },
		func() error { return m.Foreground(ctx) },
		func() error { return m.Succeed(ctx) },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		evs, err := m.Events(ctx)
		if err != nil {
			t.Fatal(err)
		}
		fromLog, err := session.Replay(evs)
		if err != nil {
			t.Fatal(err)
		}
		live, err := m.State(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if fromLog != live {
			t.Fatalf("step %d: replay %+v live %+v", i, fromLog, live)
		}
	}
	st, err := m.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != session.StatusDone || st.Attachment != session.AttachmentAttached {
		t.Fatalf("final %+v", st)
	}
	if err := m.Start(ctx); err == nil {
		t.Fatal("start after done: expected error")
	}
}

func TestReplayEmpty(t *testing.T) {
	t.Parallel()
	if _, err := session.Replay(nil); err == nil || !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("empty replay: %v", err)
	}
}

func TestReplayCorruptStops(t *testing.T) {
	t.Parallel()
	id := mustID(t, "sess-corrupt")
	evs := []session.Event{
		{SessionID: id, Type: session.EventCreated},
		{SessionID: id, Type: session.EventStarted},
		{SessionID: id, Type: session.EventStarted},
	}
	if _, err := session.Replay(evs); err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultCompletionContract(t *testing.T) {
	t.Parallel()
	m, _ := mustNew(t, "sess-contract")
	ctx := t.Context()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Background(ctx, nil); err != nil {
		t.Fatal(err)
	}
	st, err := m.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := session.DefaultCompletionContract()
	if !st.Contract.Finish || !st.Contract.CommentOnIssue || !st.Contract.PingOnlyOnBlockers {
		t.Fatalf("contract %+v want %+v", st.Contract, want)
	}
	if st.Attachment != session.AttachmentBackground {
		t.Fatalf("attachment %s", st.Attachment)
	}
}

func TestCustomCompletionContract(t *testing.T) {
	t.Parallel()
	m, _ := mustNew(t, "sess-custom-contract")
	ctx := t.Context()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	c := session.CompletionContract{Finish: true}
	if err := m.Background(ctx, &c); err != nil {
		t.Fatal(err)
	}
	st, err := m.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Contract != c {
		t.Fatalf("contract %+v want %+v", st.Contract, c)
	}
}

func TestEventsCopy(t *testing.T) {
	t.Parallel()
	m, _ := mustNew(t, "sess-copy")
	ctx := t.Context()
	first, err := m.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first[0].Type = session.EventTerminal
	second, err := m.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Type != session.EventCreated {
		t.Fatal("Events() exposed internal log")
	}
}

func TestFailFromPending(t *testing.T) {
	t.Parallel()
	m, _ := mustNew(t, "sess-fail-pending")
	ctx := t.Context()
	if err := m.Fail(ctx, "never started"); err != nil {
		t.Fatal(err)
	}
	st, err := m.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != session.StatusFailed {
		t.Fatalf("status %s", st.Status)
	}
}
