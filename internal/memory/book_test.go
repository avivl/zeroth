package memory

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

func TestHumanWriteAndHistory(t *testing.T) {
	t.Parallel()
	b := NewBook()
	b.now = func() time.Time { return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC) }

	fact, err := b.Write(Human("alice"), KindOperator, "", "style.tests", "prefer table tests", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if fact.Key != "style.tests" || fact.Provenance.Who.Name != "alice" {
		t.Fatalf("fact %+v", fact)
	}
	_, err = b.Write(Human("bob"), KindOperator, "", "style.tests", "prefer table tests and t.Parallel", "operator")
	if err != nil {
		t.Fatal(err)
	}
	hist, err := b.History(KindOperator, "", "style.tests")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("history %d", len(hist))
	}
	if hist[0].Author.Name != "alice" || hist[1].Author.Name != "bob" {
		t.Fatalf("authors %+v", hist)
	}
	if hist[0].Action != ActionWrite || hist[1].Action != ActionEdit {
		t.Fatalf("actions %+v", hist)
	}
	d, err := b.DiffView(KindOperator, "", "style.tests", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Unified, "-prefer table tests") || !strings.Contains(d.Unified, "+prefer table tests and t.Parallel") {
		t.Fatalf("diff %q", d.Unified)
	}
}

func TestAgentWriteIsDenied(t *testing.T) {
	t.Parallel()
	b := NewBook()
	_, err := b.Write(AgentActor("harness"), KindOperator, "", "secret", "should not land", "run-1")
	if !errors.Is(err, ErrAgentCannotWrite) {
		t.Fatalf("err %v", err)
	}
	if len(b.Slice()) != 0 {
		t.Fatal("notebook was mutated")
	}
}

func TestProposeThenAccept(t *testing.T) {
	t.Parallel()
	b := NewBook()
	p, err := b.Propose(AgentActor("claude"), KindOperator, "", "kernel.rule", "policy outranks the harness", "run-1", store.SessionID{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusPending {
		t.Fatalf("status %s", p.Status)
	}
	if len(b.Slice()) != 0 {
		t.Fatal("pending proposal leaked into notebook")
	}
	_, _, err = b.Accept(AgentActor("claude"), p.ID)
	if !errors.Is(err, ErrAgentCannotWrite) {
		t.Fatalf("agent accept: %v", err)
	}
	got, fact, err := b.Accept(Human("alice"), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusAccepted || fact.Key != "kernel.rule" {
		t.Fatalf("accepted %+v fact %+v", got, fact)
	}
	if !fact.Provenance.Who.IsHuman() {
		t.Fatal("accepted fact not human-authored")
	}
	if !strings.Contains(fact.Provenance.Source, p.ID.String()) {
		t.Fatalf("source %q", fact.Provenance.Source)
	}
	slice := b.Slice()
	if len(slice) != 1 || slice[0].Body != "policy outranks the harness" {
		t.Fatalf("slice %+v", slice)
	}
	_, _, err = b.Accept(Human("alice"), p.ID)
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("second accept: %v", err)
	}
}

func TestRejectNeverEntersNotebook(t *testing.T) {
	t.Parallel()
	b := NewBook()
	p, err := b.Propose(AgentActor("claude"), KindOperator, "", "noise", "ignore me", "", store.SessionID{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Reject(Human("alice"), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRejected {
		t.Fatalf("status %s", got.Status)
	}
	if len(b.Slice()) != 0 {
		t.Fatal("rejected proposal entered notebook")
	}
	_, err = b.Get(KindOperator, "", "noise")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
}

func TestHumanCannotPropose(t *testing.T) {
	t.Parallel()
	b := NewBook()
	_, err := b.Propose(Human("alice"), KindOperator, "", "k", "b", "", store.SessionID{})
	if !errors.Is(err, ErrHumanCannotPropose) {
		t.Fatalf("err %v", err)
	}
}

func TestDeleteRemovesFromSliceKeepsHistory(t *testing.T) {
	t.Parallel()
	b := NewBook()
	if _, err := b.Write(Human("alice"), KindOperator, "", "temp.fact", "remember this", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Delete(Human("alice"), KindOperator, "", "temp.fact", "operator"); err != nil {
		t.Fatal(err)
	}
	if len(b.Slice()) != 0 {
		t.Fatal("deleted fact still in slice")
	}
	hist, err := b.History(KindOperator, "", "temp.fact")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[1].Action != ActionDelete || hist[1].Author.Name != "alice" {
		t.Fatalf("history %+v", hist)
	}
}

func TestSplitKeyBody(t *testing.T) {
	t.Parallel()
	key, body := SplitKeyBody("key: style.tests\n\nprefer table tests")
	if key != "style.tests" || body != "prefer table tests" {
		t.Fatalf("%q %q", key, body)
	}
	key, body = SplitKeyBody("just a body")
	if key != "" || body != "just a body" {
		t.Fatalf("%q %q", key, body)
	}
}
