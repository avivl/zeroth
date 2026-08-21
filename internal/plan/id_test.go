package plan

import (
	"testing"
	"time"
)

func TestParseID(t *testing.T) {
	t.Parallel()
	if _, err := ParseID(""); err == nil {
		t.Fatal("empty id")
	}
	id, err := ParseID("plan-1")
	if err != nil || id.String() != "plan-1" || id.IsZero() {
		t.Fatalf("%+v err=%v", id, err)
	}
	n, err := NewID()
	if err != nil || n.IsZero() || n.String() == id.String() {
		t.Fatalf("NewID %+v err=%v", n, err)
	}
}

func TestApproveExpired(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	if _, err := p.Approve(p.ExpiresAt.Add(time.Second)); err == nil {
		t.Fatal("expired plan must not approve")
	}
}
