package memory_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
)

type rec struct {
	action, target, resource, actor string
}

type recorder struct {
	items []rec
}

func (r *recorder) Append(_ context.Context, action, target, resourceID, actor string) error {
	r.items = append(r.items, rec{action, target, resourceID, actor})
	return nil
}

func TestStoreNotebookProposeAcceptRejectAudited(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	audit := &recorder{}
	nb, err := memory.NewNotebook(st, audit)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	p, err := nb.Propose(ctx, memory.AgentActor("claude"), memory.KindOperator, "", "kernel.rule", "deny by default", "run-1", store.SessionID{})
	if err != nil {
		t.Fatal(err)
	}
	slice, err := nb.Slice(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(slice) != 0 {
		t.Fatal("proposal leaked into notebook")
	}
	_, fact, err := nb.Accept(ctx, memory.Human("alice"), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fact.Body != "deny by default" || !fact.Provenance.Who.IsHuman() {
		t.Fatalf("fact %+v", fact)
	}
	hist, err := nb.History(ctx, memory.KindOperator, "", "kernel.rule")
	if err != nil || len(hist) != 1 || hist[0].Author.Name != "alice" {
		t.Fatalf("history %+v err=%v", hist, err)
	}
	p2, err := nb.Propose(ctx, memory.AgentActor("claude"), memory.KindOperator, "", "noise", "nope", "", store.SessionID{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nb.Reject(ctx, memory.Human("alice"), p2.ID); err != nil {
		t.Fatal(err)
	}
	slice, err = nb.Slice(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(slice) != 1 || slice[0].Key != "kernel.rule" {
		t.Fatalf("slice %+v", slice)
	}
	if _, err := nb.Write(ctx, memory.AgentActor("claude"), memory.KindOperator, "", "x", "y", ""); !errors.Is(err, memory.ErrAgentCannotWrite) {
		t.Fatalf("agent write: %v", err)
	}
	var sawAccept, sawReject, sawPropose bool
	for _, item := range audit.items {
		switch item.action {
		case "memory.propose":
			sawPropose = true
		case "memory.propose.accept":
			sawAccept = true
		case "memory.propose.reject":
			sawReject = true
		}
		if strings.Contains(item.action, "accept") && item.actor != "alice" {
			t.Fatalf("accept actor %q", item.actor)
		}
	}
	if !sawPropose || !sawAccept || !sawReject {
		t.Fatalf("audit %+v", audit.items)
	}
}

func TestStoreNotebookDeleteDropsFromSlice(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	nb, err := memory.NewNotebook(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := nb.Write(ctx, memory.Human("alice"), memory.KindOperator, "", "tmp", "gone soon", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := nb.Delete(ctx, memory.Human("alice"), memory.KindOperator, "", "tmp", "operator"); err != nil {
		t.Fatal(err)
	}
	slice, err := nb.Slice(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(slice) != 0 {
		t.Fatalf("slice %+v", slice)
	}
	compiled := memory.Compile(slice)
	if strings.Contains(compiled, "tmp") || strings.Contains(compiled, "gone soon") {
		t.Fatalf("compiled deleted fact: %s", compiled)
	}
}
