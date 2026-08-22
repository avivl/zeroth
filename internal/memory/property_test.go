package memory

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/avivl/zeroth/internal/store"
)

const propertyIterations = 2000

// TestPropertyAgentProposalNeverReachesNotebookWithoutHumanAccept is the
// Z1-022 invariant: no sequence of notebook operations lets an agent
// author a live fact. Pending and rejected proposals stay out. Every
// revision on a fact is human-authored.
func TestPropertyAgentProposalNeverReachesNotebookWithoutHumanAccept(t *testing.T) {
	r := rand.New(rand.NewSource(22))
	humans := []Actor{Human("alice"), Human("bob"), Human("cara")}
	agents := []Actor{AgentActor("claude"), AgentActor("cursor"), AgentActor("other")}
	keys := []string{"k0", "k1", "k2", "k3", "k4"}
	var zeroSess store.SessionID
	var zeroProp store.MemoryProposalID

	for i := 0; i < propertyIterations; i++ {
		b := NewBook()
		var pending []store.MemoryProposalID
		for op := 0; op < 12; op++ {
			key := keys[r.Intn(len(keys))]
			body := fmt.Sprintf("body-%d-%d", i, op)
			switch r.Intn(6) {
			case 0:
				h := humans[r.Intn(len(humans))]
				_, _ = b.Write(h, KindOperator, "", key, body, "op")
			case 1:
				a := agents[r.Intn(len(agents))]
				p, err := b.Propose(a, KindOperator, "", key, body, "run", zeroSess)
				if err == nil {
					pending = append(pending, p.ID)
				}
			case 2:
				if len(pending) == 0 {
					continue
				}
				id := pending[r.Intn(len(pending))]
				h := humans[r.Intn(len(humans))]
				_, _, _ = b.Accept(h, id)
			case 3:
				if len(pending) == 0 {
					continue
				}
				id := pending[r.Intn(len(pending))]
				h := humans[r.Intn(len(humans))]
				_, _ = b.Reject(h, id)
			case 4:
				h := humans[r.Intn(len(humans))]
				_, _ = b.Delete(h, KindOperator, "", key, "op")
			default:
				a := agents[r.Intn(len(agents))]
				if _, err := b.Write(a, KindOperator, "", key, body, "run"); err == nil {
					t.Fatalf("iteration %d: agent write succeeded", i)
				}
				target := zeroProp
				if len(pending) > 0 {
					target = pending[r.Intn(len(pending))]
				}
				if _, _, err := b.Accept(a, target); err == nil {
					t.Fatalf("iteration %d: agent accept succeeded", i)
				}
				if _, err := b.Reject(a, target); err == nil {
					t.Fatalf("iteration %d: agent reject succeeded", i)
				}
				if _, err := b.Delete(a, KindOperator, "", key, "run"); err == nil {
					t.Fatalf("iteration %d: agent delete succeeded", i)
				}
			}
		}
		assertHumanOnlyNotebook(t, i, b)
	}
}

func assertHumanOnlyNotebook(t *testing.T, i int, b *Book) {
	t.Helper()
	for _, f := range b.Slice() {
		if f.Deleted {
			t.Fatalf("iteration %d: deleted fact in slice", i)
		}
		if !f.Provenance.Who.IsHuman() {
			t.Fatalf("iteration %d: live fact %q authored by %s", i, f.Key, f.Provenance.Who.Kind)
		}
		for _, rev := range f.Versions {
			if !rev.Author.IsHuman() {
				t.Fatalf("iteration %d: revision %d of %q authored by agent %q", i, rev.Version, f.Key, rev.Author.Name)
			}
		}
	}
	for _, p := range b.Proposals(StatusPending) {
		if !p.MemoryID.IsZero() {
			t.Fatalf("iteration %d: pending proposal %s has memory id", i, p.ID.String())
		}
	}
	for _, p := range b.Proposals(StatusRejected) {
		if !p.MemoryID.IsZero() {
			t.Fatalf("iteration %d: rejected proposal %s created a fact", i, p.ID.String())
		}
	}
	for _, p := range b.Proposals(StatusAccepted) {
		if p.MemoryID.IsZero() {
			t.Fatalf("iteration %d: accepted proposal %s has no fact", i, p.ID.String())
		}
		fact, err := b.Get(p.Kind, p.RefID, p.Key)
		if err != nil {
			t.Fatalf("iteration %d: accepted proposal %s missing fact: %v", i, p.ID.String(), err)
		}
		if !fact.Provenance.Who.IsHuman() {
			t.Fatalf("iteration %d: accepted fact not human", i)
		}
	}
}
