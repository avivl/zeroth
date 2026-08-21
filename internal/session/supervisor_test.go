package session_test

import (
	"errors"
	"sync"
	"testing"
	"testing/quick"
	"time"

	"github.com/avivl/zeroth/internal/session"
)

func TestSupervisorLifecycle(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	sup, err := session.NewSupervisor(log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sup.Close)
	ctx := t.Context()

	id, err := sup.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sup.Live(id) {
		t.Fatal("expected live goroutine")
	}
	st, err := sup.State(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != session.StatusRunning || st.Attachment != session.AttachmentAttached {
		t.Fatalf("start %+v", st)
	}
	if err := sup.Steer(ctx, id, "do the thing"); err != nil {
		t.Fatal(err)
	}
	if err := sup.Background(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	st, err = sup.State(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Attachment != session.AttachmentBackground || !st.Contract.PingOnlyOnBlockers {
		t.Fatalf("background %+v", st)
	}
	if err := sup.Foreground(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := sup.Succeed(ctx, id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for sup.Live(id) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sup.Live(id) {
		t.Fatal("goroutine still live after succeed")
	}
	st, err = sup.State(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != session.StatusDone {
		t.Fatalf("done %+v", st)
	}
}

func TestSupervisorStartWith(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	sup, err := session.NewSupervisor(log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sup.Close)
	id := mustID(t, "s_startwith")
	if err := sup.StartWith(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if !sup.Live(id) {
		t.Fatal("expected live goroutine")
	}
	ids := sup.LiveIDs()
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("LiveIDs = %v", ids)
	}
	if err := sup.StartWith(t.Context(), id); err == nil {
		t.Fatal("expected already-live error")
	}
	if err := sup.Succeed(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for sup.Live(id) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := sup.Background(t.Context(), id, nil); !errors.Is(err, session.ErrIllegalTransition) {
		t.Fatalf("background after succeed: %v", err)
	}
}

func TestDaemonRestartResumesFromLog(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := session.NewMemoryLog()
	sup, err := session.NewSupervisor(log)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sup.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := sup.EmitToken(ctx, id, "tok-1"); err != nil {
		t.Fatal(err)
	}
	if err := sup.ProposePlan(ctx, id, "plan-1"); err != nil {
		t.Fatal(err)
	}
	if err := sup.RequestApproval(ctx, id, "plan-1"); err != nil {
		t.Fatal(err)
	}
	if err := sup.Background(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	before, err := sup.State(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := sup.Events(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sup.Close()

	// New process: new supervisor, same durable events.
	cloned, err := session.NewMemoryLogFrom(log.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := session.Restore(ctx, cloned)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)

	after, err := restored.State(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("restart state %+v want %+v", after, before)
	}
	if !restored.Live(id) {
		t.Fatal("non-terminal session was not resumed")
	}
	got, err := restored.Events(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(beforeEvents) {
		t.Fatalf("event count %d want %d", len(got), len(beforeEvents))
	}
	for i := range got {
		if got[i].Type != beforeEvents[i].Type || got[i].Payload != beforeEvents[i].Payload || got[i].Seq != beforeEvents[i].Seq {
			t.Fatalf("events[%d] = %+v want %+v", i, got[i], beforeEvents[i])
		}
	}
	replayed, err := session.Replay(got)
	if err != nil {
		t.Fatal(err)
	}
	if replayed != after {
		t.Fatalf("replay after restore %+v want %+v", replayed, after)
	}

	// The resumed session still accepts steer and can finish.
	if err := restored.Steer(ctx, id, "after restart"); err != nil {
		t.Fatal(err)
	}
	if err := restored.BeginApply(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := restored.Succeed(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSkipsTerminal(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := session.NewMemoryLog()
	sup, err := session.NewSupervisor(log)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sup.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := sup.Fail(ctx, id, "boom"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for sup.Live(id) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	sup.Close()

	restored, err := session.Restore(ctx, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	if restored.Live(id) {
		t.Fatal("terminal session should not get a goroutine")
	}
	st, err := restored.State(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != session.StatusFailed {
		t.Fatalf("status %s", st.Status)
	}
}

func TestConcurrentAttachAndSteer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := session.NewMemoryLog()
	sup, err := session.NewSupervisor(log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sup.Close)
	id, err := sup.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			replay, live, stop, err := sup.Tail(ctx, id, 50)
			if err != nil {
				t.Errorf("tail: %v", err)
				return
			}
			defer stop()
			_ = replay
			select {
			case <-live:
			case <-time.After(2 * time.Second):
			}
		}()
		go func(i int) {
			defer wg.Done()
			if err := sup.Steer(ctx, id, "steer"); err != nil {
				t.Errorf("steer %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	st, err := sup.State(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != session.StatusRunning {
		t.Fatalf("status %s", st.Status)
	}
	evs, err := sup.Events(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	steers := 0
	for _, ev := range evs {
		if ev.Type == session.EventSteered {
			steers++
		}
	}
	if steers != n {
		t.Fatalf("steers %d want %d", steers, n)
	}
}

func TestPropertyReplayDeterministic(t *testing.T) {
	t.Parallel()
	fn := func(steps []uint8) bool {
		if len(steps) > 64 {
			steps = steps[:64]
		}
		log := session.NewMemoryLog()
		id, err := session.NewID()
		if err != nil {
			return false
		}
		ctx := t.Context()
		m, err := session.New(ctx, id, log)
		if err != nil {
			return false
		}
		ops := []func() error{
			func() error { return m.Start(ctx) },
			func() error { return m.EmitToken(ctx, "t") },
			func() error { return m.EmitToolCall(ctx, "tool") },
			func() error { return m.ProposePlan(ctx, "p") },
			func() error { return m.RequestApproval(ctx, "p") },
			func() error { return m.RequestChanges(ctx) },
			func() error { return m.BeginApply(ctx) },
			func() error { return m.TakeCheckpoint(ctx, "c") },
			func() error { return m.ReportError(ctx, "e") },
			func() error { return m.Steer(ctx, "s") },
			func() error { return m.Background(ctx, nil) },
			func() error { return m.Foreground(ctx) },
			func() error { return m.Succeed(ctx) },
			func() error { return m.Fail(ctx, "f") },
		}
		for _, step := range steps {
			_ = ops[int(step)%len(ops)]()
			evs, err := m.Events(ctx)
			if err != nil {
				return false
			}
			live, err := m.State(ctx)
			if err != nil {
				return false
			}
			again, err := session.Replay(evs)
			if err != nil || again != live {
				return false
			}
			again2, err := session.Replay(evs)
			if err != nil || again2 != again {
				return false
			}
		}
		return true
	}
	if err := quick.Check(fn, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func TestNewSupervisorNilLog(t *testing.T) {
	t.Parallel()
	if _, err := session.NewSupervisor(nil); err == nil {
		t.Fatal("expected error")
	}
}
