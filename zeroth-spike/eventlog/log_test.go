package eventlog_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/eventlog"
	"github.com/avivl/zeroth/zeroth-spike/session"
	_ "modernc.org/sqlite"
)

func TestEventsTriggerRejectsUpdateAndDelete(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "events.db")
	log, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id := mustID(t, "sess-trigger")
	if _, err := log.Append(context.Background(), id, eventlog.TypeToken, "keep"); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`UPDATE events SET payload = 'mutated'`); err == nil {
		t.Fatal("UPDATE events: expected append-only abort")
	}
	if _, err := db.Exec(`DELETE FROM events`); err == nil {
		t.Fatal("DELETE events: expected append-only abort")
	}
}

func TestWALMode(t *testing.T) {
	t.Parallel()
	log := openLog(t)
	mode, err := log.JournalMode()
	if err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestAppendReplayAndAfter(t *testing.T) {
	t.Parallel()
	log := openLog(t)
	ctx := context.Background()
	id := mustID(t, "sess-replay")

	for i := 0; i < 5; i++ {
		if _, err := log.Append(ctx, id, eventlog.TypeToken, "x"); err != nil {
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

func TestEmptyBatchRejected(t *testing.T) {
	t.Parallel()
	log := openLog(t)
	ctx := context.Background()
	id := mustID(t, "sess-append-only")
	ev, err := log.Append(ctx, id, eventlog.TypeCreated, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = log.AppendBatch(ctx, id, nil)
	if err == nil {
		t.Fatal("empty batch: expected error")
	}
	// Use After to prove the row exists, then the trigger is the contract.
	got, err := log.After(ctx, id, ev.Seq-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Seq != ev.Seq {
		t.Fatalf("row missing after append: %+v", got)
	}
}

func TestSecondOpenReadsFirstWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.db")
	ctx := context.Background()
	id := mustID(t, "sess-cross-conn")

	a, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Append(ctx, id, eventlog.TypeToken, "from-a"); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	got, err := b.ReplayLast(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Payload != "from-a" {
		t.Fatalf("second open did not see SQLite row: %+v", got)
	}
}

func TestSubscribeWakesTail(t *testing.T) {
	t.Parallel()
	log := openLog(t)
	ctx := context.Background()
	id := mustID(t, "sess-wake")

	wait, unsub := log.Subscribe(id)
	defer unsub()

	errc := make(chan error, 1)
	go func() {
		errc <- wait(ctx)
	}()
	time.Sleep(20 * time.Millisecond)
	if _, err := log.Append(ctx, id, eventlog.TypeToken, "hi"); err != nil {
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

func TestConcurrentAppends(t *testing.T) {
	t.Parallel()
	log := openLog(t)
	ctx := context.Background()
	id := mustID(t, "sess-race")

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := log.Append(ctx, id, eventlog.TypeToken, "t"); err != nil {
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

func TestCreateAndGetSession(t *testing.T) {
	t.Parallel()
	log := openLog(t)
	ctx := context.Background()
	id := mustID(t, "sess-meta")
	if err := log.CreateSession(ctx, id, "running", true); err != nil {
		t.Fatal(err)
	}
	row, found, err := log.GetSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !row.Foreground || row.Status != "running" {
		t.Fatalf("row = %+v found=%v", row, found)
	}
	if err := log.SetSession(ctx, id, "running", false); err != nil {
		t.Fatal(err)
	}
	row, _, err = log.GetSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Foreground {
		t.Fatal("expected backgrounded")
	}
}

func openLog(t *testing.T) *eventlog.Log {
	t.Helper()
	log, err := eventlog.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

func mustID(t *testing.T, raw string) session.ID {
	t.Helper()
	id, err := session.ParseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
