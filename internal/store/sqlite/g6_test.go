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
// Append (or one batched AppendEvents commit). The spike bar was 50 ms on a
// quiet machine, recorded at p50=0.089 ms max=5.714 ms.
//
// Two bounds, because they catch different things (42-80). g6SustainedLimit
// is p95: if most writes are slow, the store regressed. g6SpikeLimit is the
// single worst sample, and it is deliberately loose, because max of 100
// wall-clock samples on shared CI is the least stable statistic available
// and one scheduling artifact should not fail an unrelated PR. A real
// multi-hundred-ms stall, which is what the original gate was written to
// catch, still trips it.
//
// Gating on max alone failed PR #75, a branch touching no store code:
// p50=5.8 ms p95=89.7 ms p99=126 ms max=126 ms. Same commit passed on main
// minutes later. Locally this test sees max 8-19 ms with the whole package
// running.
const (
	g6SustainedLimit = 100 * time.Millisecond
	g6SpikeLimit     = 500 * time.Millisecond
)

// Deliberately not t.Parallel: this measures wall-clock latency, and Go
// runs non-parallel tests outside the parallel phase, so it is not competing
// with every other test in this package for the same cores.
func TestG6WriteStall(t *testing.T) {
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
	t.Logf("G6 write stall n=%d p50=%s p95=%s p99=%s max=%s (gates: p95 <= %s, max <= %s; recorded p50=0.089ms max=5.714ms)",
		len(stalls), p50, p95, p99, max, g6SustainedLimit, g6SpikeLimit)
	if p95 > g6SustainedLimit {
		t.Fatalf("G6 p95 stall %s, want <= %s (writes are slow across the board, not one hiccup)", p95, g6SustainedLimit)
	}
	if max > g6SpikeLimit {
		t.Fatalf("G6 max stall %s, want <= %s", max, g6SpikeLimit)
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
