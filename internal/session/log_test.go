package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/session"
)

func TestAppendReplayAndAfter(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	ctx := t.Context()
	id := mustID(t, "sess-replay-log")

	for i := 0; i < 5; i++ {
		if _, err := log.Append(ctx, id, session.EventToken, "x"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := log.ReplayLast(ctx, id, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("replay last 3: len=%d", len(got))
	}
	if got[0].Seq >= got[1].Seq || got[1].Seq >= got[2].Seq {
		t.Fatalf("replay not chronological: %+v", got)
	}
	after, err := log.After(ctx, id, got[0].Seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Fatalf("after seq %d: len=%d, want 2", got[0].Seq, len(after))
	}
}

func TestReplayLastRejectsZero(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	if _, err := log.ReplayLast(t.Context(), mustID(t, "sess-n"), 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestSubscribeWakesTail(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	ctx := t.Context()
	id := mustID(t, "sess-wake")

	wait, unsub := log.Subscribe(id)
	defer unsub()

	errc := make(chan error, 1)
	go func() {
		errc <- wait(ctx)
	}()
	time.Sleep(20 * time.Millisecond)
	if _, err := log.Append(ctx, id, session.EventToken, "hi"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tail did not wake after append")
	}
}

func TestMemoryLogFromPreservesSeq(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	ctx := t.Context()
	id := mustID(t, "sess-from")
	if _, err := log.Append(ctx, id, session.EventCreated, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(ctx, id, session.EventStarted, ""); err != nil {
		t.Fatal(err)
	}
	snap := log.Snapshot()
	restored, err := session.NewMemoryLogFrom(snap)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restored.After(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq != snap[0].Seq || got[1].Type != session.EventStarted {
		t.Fatalf("restored %+v snap %+v", got, snap)
	}
}

func TestConcurrentAppends(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	ctx := t.Context()
	id := mustID(t, "sess-race-log")

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := log.Append(ctx, id, session.EventToken, "t"); err != nil {
				t.Errorf("append: %v", err)
			}
		}()
	}
	wg.Wait()
	got, err := log.ReplayLast(ctx, id, n+8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("len=%d, want %d", len(got), n)
	}
}

func TestTailReplayThenLive(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	ctx := t.Context()
	id := mustID(t, "sess-tail")
	if _, err := log.Append(ctx, id, session.EventCreated, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(ctx, id, session.EventStarted, ""); err != nil {
		t.Fatal(err)
	}

	replay, live, stop, err := session.Tail(ctx, log, id, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if len(replay) != 2 || replay[0].Type != session.EventCreated || replay[1].Type != session.EventStarted {
		t.Fatalf("replay %+v", replay)
	}

	if _, err := log.Append(ctx, id, session.EventToken, "live-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-live:
		if ev.Type != session.EventToken || ev.Payload != "live-1" {
			t.Fatalf("live %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive live event")
	}
}

func TestCancelledAppend(t *testing.T) {
	t.Parallel()
	log := session.NewMemoryLog()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := log.Append(ctx, mustID(t, "sess-cancel"), session.EventToken, "x"); err == nil {
		t.Fatal("expected error")
	}
}
