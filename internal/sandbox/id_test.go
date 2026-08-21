package sandbox_test

import (
	"testing"

	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/session"
)

func TestNewID(t *testing.T) {
	t.Parallel()
	a, err := sandbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := sandbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if a.IsZero() || a.String() == b.String() {
		t.Fatalf("NewID not unique: %q %q", a.String(), b.String())
	}
	parsed, err := sandbox.ParseID(a.String())
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
		{name: "ok", raw: "sbx-1"},
		{name: "empty", raw: "", wantErr: true},
		{name: "whitespace", raw: "   ", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, err := sandbox.ParseID(tc.raw)
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

func TestIDIsNotSessionID(t *testing.T) {
	t.Parallel()
	sid, err := session.ParseID("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	bid, err := sandbox.ParseID("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	// Same raw string, distinct types. Mixing them is a compile error
	// (bid = sid does not compile). This test locks the helpers.
	if sid.String() != bid.String() {
		t.Fatal("raw strings should match")
	}
}
