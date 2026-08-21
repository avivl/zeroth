package sqlite

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

// G6 from the BA-6 spike: 5 concurrent sessions appending. A stall is one
// Append (or one batched AppendEvents commit). Pass bar: no stall > 50 ms.
// Sample count is smaller than the spike's 110 so race-instrumented CI
// stays inside the job budget; the gate is still max stall, not a percentile.
const g6StallLimit = 50 * time.Millisecond

func TestG6WriteStall(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	agent := store.Agent{ID: mustAID(t, "g6-agent"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}

	const sessions = 5
	const warmup = 5
	const samples = 20
	ids := make([]store.SessionID, sessions)
	for i := 0; i < sessions; i++ {
		id, err := store.ParseSessionID("g6-" + itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
		if err := s.CreateSession(ctx, store.Session{ID: id, AgentID: agent.ID, Status: "running"}); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var stalls []time.Duration
	var wg sync.WaitGroup
	wg.Add(sessions)
	for i := 0; i < sessions; i++ {
		go func(id store.SessionID) {
			defer wg.Done()
			for n := 0; n < warmup+samples; n++ {
				start := time.Now()
				_, err := s.AppendEvent(ctx, id, store.Event{Type: "log", Message: "t"})
				d := time.Since(start)
				if err != nil {
					t.Errorf("append: %v", err)
					return
				}
				if n >= warmup {
					mu.Lock()
					stalls = append(stalls, d)
					mu.Unlock()
				}
			}
		}(ids[i])
	}
	wg.Wait()
	if t.Failed() {
		return
	}
	if len(stalls) != sessions*samples {
		t.Fatalf("n=%d, want %d", len(stalls), sessions*samples)
	}
	sort.Slice(stalls, func(i, j int) bool { return stalls[i] < stalls[j] })
	p50 := stalls[len(stalls)*50/100]
	p95 := stalls[len(stalls)*95/100]
	p99 := stalls[len(stalls)*99/100]
	max := stalls[len(stalls)-1]
	t.Logf("G6 write stall n=%d p50=%s p95=%s p99=%s max=%s (spike gate: max <= %s, recorded p50=0.089ms max=5.714ms)",
		len(stalls), p50, p95, p99, max, g6StallLimit)
	if max > g6StallLimit {
		t.Fatalf("G6 max stall %s, want <= %s", max, g6StallLimit)
	}
}

func TestRangeReadIndexed(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	agent := store.Agent{ID: mustAID(t, "rr-agent"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	id, err := store.ParseSessionID("rr-sess")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, store.Session{ID: id, AgentID: agent.ID, Status: "running"}); err != nil {
		t.Fatal(err)
	}
	const n = 500
	batch := make([]store.Event, 0, 100)
	for i := 0; i < n; i++ {
		batch = append(batch, store.Event{Type: "log", Message: "x"})
		if len(batch) == 100 {
			if _, err := s.AppendEvents(ctx, id, batch); err != nil {
				t.Fatal(err)
			}
			batch = batch[:0]
		}
	}

	start := time.Now()
	got, err := s.ReplayLast(ctx, id, 20)
	replay := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Fatalf("replay len=%d", len(got))
	}
	start = time.Now()
	after, err := s.EventsAfter(ctx, id, got[0].Seq)
	afterD := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 19 {
		t.Fatalf("after len=%d", len(after))
	}
	t.Logf("range-read %d rows: ReplayLast(20)=%s EventsAfter=%s (attach replay is the G1 hot path)", n, replay, afterD)
	if replay > 50*time.Millisecond {
		t.Fatalf("ReplayLast too slow: %s", replay)
	}
}

func BenchmarkAppend(b *testing.B) {
	s, err := New(filepath.Join(b.TempDir(), "zeroth.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	agent := store.Agent{ID: mustAID(b, "b-agent"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		b.Fatal(err)
	}
	id, err := store.ParseSessionID("b-sess")
	if err != nil {
		b.Fatal(err)
	}
	if err := s.CreateSession(ctx, store.Session{ID: id, AgentID: agent.ID, Status: "running"}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.AppendEvent(ctx, id, store.Event{Type: "log", Message: "t"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReplayLast(b *testing.B) {
	s, err := New(filepath.Join(b.TempDir(), "zeroth.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	agent := store.Agent{ID: mustAID(b, "b-agent"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		b.Fatal(err)
	}
	id, err := store.ParseSessionID("b-sess")
	if err != nil {
		b.Fatal(err)
	}
	if err := s.CreateSession(ctx, store.Session{ID: id, AgentID: agent.ID, Status: "running"}); err != nil {
		b.Fatal(err)
	}
	batch := make([]store.Event, 100)
	for i := range batch {
		batch[i] = store.Event{Type: "log", Message: "x"}
	}
	for i := 0; i < 20; i++ {
		if _, err := s.AppendEvents(ctx, id, batch); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ReplayLast(ctx, id, 20); err != nil {
			b.Fatal(err)
		}
	}
}

func itoa(i int) string {
	return []string{"0", "1", "2", "3", "4"}[i]
}
