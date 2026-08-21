package session_test

import (
	"testing"

	"github.com/avivl/zeroth/internal/session"
)

func TestNewID(t *testing.T) {
	t.Parallel()
	a, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if a.IsZero() || a.String() == b.String() {
		t.Fatalf("NewID not unique: %q %q", a.String(), b.String())
	}
	parsed, err := session.ParseID(a.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != a {
		t.Fatalf("ParseID round-trip: got %q want %q", parsed.String(), a.String())
	}
}

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

func TestNewRejectsZeroID(t *testing.T) {
	t.Parallel()
	if _, err := session.New(t.Context(), session.ID{}, session.NewMemoryLog()); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRejectsNilLog(t *testing.T) {
	t.Parallel()
	id := mustID(t, "sess-nil-log")
	if _, err := session.New(t.Context(), id, nil); err == nil {
		t.Fatal("expected error")
	}
}

func mustID(t *testing.T, raw string) session.ID {
	t.Helper()
	id, err := session.ParseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustNew(t *testing.T, raw string) (*session.Machine, *session.MemoryLog) {
	t.Helper()
	log := session.NewMemoryLog()
	m, err := session.New(t.Context(), mustID(t, raw), log)
	if err != nil {
		t.Fatal(err)
	}
	return m, log
}
