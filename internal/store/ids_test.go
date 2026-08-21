package store

import (
	"testing"
	"time"
)

func TestParseIDRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := ParseSessionID(""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParsePlanID("   "); err == nil {
		t.Fatal("expected error")
	}
}

func TestIDsAreNotInterchangeableAtTheTypeLevel(t *testing.T) {
	t.Parallel()
	s, err := ParseSessionID("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePlanID("plan-1")
	if err != nil {
		t.Fatal(err)
	}
	// Same underlying string shape, distinct types. Mixing them is a
	// compile error (s = p does not compile). This test locks the helpers.
	if s.String() == p.String() {
		t.Fatal("distinct ids unexpectedly equal")
	}
	if s.IsZero() || p.IsZero() {
		t.Fatal("parsed id is zero")
	}
}

func TestClampLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want int
	}{
		{0, DefaultLimit},
		{-1, DefaultLimit},
		{1, 1},
		{200, 200},
		{201, MaxLimit},
	}
	for _, tc := range cases {
		if got := ClampLimit(tc.in); got != tc.want {
			t.Fatalf("ClampLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	ts := time.Unix(0, 1_700_000_000_000_000_123).UTC()
	raw := EncodeCursor(ts, "abc")
	got, id, err := DecodeCursor(raw)
	if err != nil {
		t.Fatal(err)
	}
	if id != "abc" || !got.Equal(ts) {
		t.Fatalf("got %s %q", got, id)
	}
	if _, _, err := DecodeCursor(""); err == nil {
		t.Fatal("empty cursor")
	}
	if _, _, err := DecodeCursor("nope"); err == nil {
		t.Fatal("bad cursor")
	}
}
