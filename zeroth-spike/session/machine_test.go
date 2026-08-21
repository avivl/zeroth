package session_test

import (
	"sync"
	"testing"

	"github.com/avivl/zeroth/zeroth-spike/session"
)

func TestParseID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "ok", raw: "sess-1"},
		{name: "empty", raw: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, err := session.ParseID(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseID: %v", err)
			}
			if id.String() != tc.raw {
				t.Fatalf("String() = %q, want %q", id.String(), tc.raw)
			}
		})
	}
}

func TestMachineLifecycle(t *testing.T) {
	t.Parallel()

	id, err := session.ParseID("sess-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	m, err := session.New(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status() != session.StatusNew {
		t.Fatalf("status = %s, want %s", m.Status(), session.StatusNew)
	}
	if err := m.Stop(); err == nil {
		t.Fatal("stop from new: expected error")
	}
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if m.Status() != session.StatusRunning {
		t.Fatalf("status = %s, want %s", m.Status(), session.StatusRunning)
	}
	if err := m.Start(); err == nil {
		t.Fatal("second start: expected error")
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if m.Status() != session.StatusStopped {
		t.Fatalf("status = %s, want %s", m.Status(), session.StatusStopped)
	}
	if err := m.Start(); err == nil {
		t.Fatal("start after stop: expected error")
	}

	got := m.Events()
	want := []session.EventType{
		session.EventCreated,
		session.EventStarted,
		session.EventStopped,
	}
	if len(got) != len(want) {
		t.Fatalf("events len = %d, want %d", len(got), len(want))
	}
	for i, typ := range want {
		if got[i].Type != typ {
			t.Fatalf("events[%d] = %s, want %s", i, got[i].Type, typ)
		}
		if got[i].At.IsZero() {
			t.Fatalf("events[%d] has zero time", i)
		}
	}
}

func TestNewRejectsZeroID(t *testing.T) {
	t.Parallel()
	if _, err := session.New(session.ID{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestEventsCopy(t *testing.T) {
	t.Parallel()
	id, err := session.ParseID("sess-copy")
	if err != nil {
		t.Fatal(err)
	}
	m, err := session.New(id)
	if err != nil {
		t.Fatal(err)
	}
	first := m.Events()
	first[0].Type = session.EventStopped
	second := m.Events()
	if second[0].Type != session.EventCreated {
		t.Fatal("Events() exposed internal log")
	}
}

func TestConcurrentEvents(t *testing.T) {
	t.Parallel()
	id, err := session.ParseID("sess-race")
	if err != nil {
		t.Fatal(err)
	}
	m, err := session.New(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Status()
			_ = m.Events()
		}()
	}
	wg.Wait()
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
}
