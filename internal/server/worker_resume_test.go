package server_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"go.uber.org/zap"
)

// resumeHarness holds its turn open so the daemon can be killed mid-run,
// and reports a vendor session the way the real driver learns one from the
// stream's first line.
type resumeHarness struct {
	mu     sync.Mutex
	vendor string
	specs  []harness.Spec
}

func (h *resumeHarness) Name() string { return "resume" }

func (h *resumeHarness) Start(_ context.Context, spec harness.Spec) (harness.Handle, error) {
	h.mu.Lock()
	h.specs = append(h.specs, spec)
	h.mu.Unlock()
	id, err := harness.NewID()
	if err != nil {
		return harness.Handle{}, err
	}
	return harness.Handle{ID: id}, nil
}

func (h *resumeHarness) Stream(ctx context.Context, _ harness.ID) (<-chan harness.Event, error) {
	ch := make(chan harness.Event)
	go func() {
		select {
		case ch <- harness.Event{Kind: harness.EventToken, Payload: "working"}:
		case <-ctx.Done():
			close(ch)
			return
		}
		<-ctx.Done() // never propose a plan; the turn stays in flight
		close(ch)
	}()
	return ch, nil
}

func (h *resumeHarness) Steer(context.Context, harness.ID, string) error { return nil }

func (h *resumeHarness) Checkpoint(context.Context, harness.ID) (harness.Checkpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return harness.Checkpoint{VendorSession: h.vendor}, nil
}

func (h *resumeHarness) Stop(context.Context, harness.ID) error { return nil }

func (h *resumeHarness) startSpecs() []harness.Spec {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]harness.Spec(nil), h.specs...)
}

// 42-78: a daemon killed mid-turn used to restart the turn from the prompt,
// losing the whole turn rather than only post-checkpoint work. The vendor
// session now survives the restart and the resumed turn continues from it.
func TestKilledTurnResumesFromStoredVendorSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zeroth.db")
	st, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	h := &resumeHarness{vendor: "vendor-session-abc"}
	cfg := func(s store.Store) server.Config {
		return server.Config{
			Store:         s,
			Log:           zap.NewNop(),
			Harness:       h,
			TokenInterval: time.Hour,
			TokenCount:    1000,
		}
	}

	srv1, err := server.New(cfg(st))
	if err != nil {
		t.Fatal(err)
	}
	hs1 := httptest.NewServer(srv1.Handler())
	run := createRun(t, hs1.URL, "resume across restart")
	sid, err := store.ParseSessionID(string(run.Id))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		sess, err := st.GetSession(t.Context(), sid)
		return err == nil && sess.HarnessSession == h.vendor
	}, "vendor session was never persisted during the turn")

	// Kill the daemon. Close appends no terminal event, so the run stays live.
	srv1.Close()
	hs1.Close()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st2, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	srv2, err := server.New(cfg(st2))
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	t.Cleanup(srv2.Close)

	waitFor(t, 3*time.Second, func() bool { return len(h.startSpecs()) >= 2 }, "the run did not resume after restart")

	specs := h.startSpecs()
	first, resumed := specs[0], specs[len(specs)-1]
	if first.Resume.VendorSession != "" {
		t.Fatalf("a new run carried a resume id: %q", first.Resume.VendorSession)
	}
	if resumed.Resume.VendorSession != h.vendor {
		t.Fatalf("resumed turn started with vendor session %q, want %q; the turn restarted from scratch",
			resumed.Resume.VendorSession, h.vendor)
	}
	if resumed.Prompt != first.Prompt {
		t.Fatalf("resumed prompt %q, want the original %q", resumed.Prompt, first.Prompt)
	}
}

// A run whose driver reports no vendor session must behave exactly as before.
func TestRunWithoutVendorSessionStartsFresh(t *testing.T) {
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := &resumeHarness{vendor: ""}
	srv, err := server.New(server.Config{
		Store:         st,
		Log:           zap.NewNop(),
		Harness:       h,
		TokenInterval: time.Hour,
		TokenCount:    1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	run := createRun(t, hs.URL, "no vendor session")
	sid, err := store.ParseSessionID(string(run.Id))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return len(h.startSpecs()) >= 1 }, "turn never started")
	time.Sleep(200 * time.Millisecond)

	sess, err := st.GetSession(t.Context(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if sess.HarnessSession != "" {
		t.Fatalf("stored a vendor session the driver never reported: %q", sess.HarnessSession)
	}
	for i, sp := range h.startSpecs() {
		if sp.Resume.VendorSession != "" {
			t.Fatalf("spec[%d] carried a resume id: %q", i, sp.Resume.VendorSession)
		}
	}
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}
