package memory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

// Auditor records a signed trail row. The real implementation is
// audit.Log; tests may use a recorder. Optional: a nil Auditor skips.
type Auditor interface {
	Append(ctx context.Context, action, target, resourceID, actor string) error
}

// Notebook is the store-backed notebook. Propose-first is enforced here
// the same way as [Book]: there is no direct agent write.
type Notebook struct {
	store store.Store
	audit Auditor
	now   func() time.Time
}

// NewNotebook returns a store-backed notebook. st is required.
func NewNotebook(st store.Store, audit Auditor) (*Notebook, error) {
	if st == nil {
		return nil, fmt.Errorf("memory notebook: nil store: %w", ErrInvalid)
	}
	return &Notebook{
		store: st,
		audit: audit,
		now:   func() time.Time { return time.Now().UTC() },
	}, nil
}

// Write is the human path. Agents calling this get ErrAgentCannotWrite.
func (n *Notebook) Write(ctx context.Context, actor Actor, kind, refID, key, body, source string) (Fact, error) {
	if err := requireHuman(actor); err != nil {
		return Fact{}, err
	}
	kind, refID, key, body, err := normalizeEntry(kind, refID, key, body)
	if err != nil {
		return Fact{}, err
	}
	now := n.now()
	prev, exists, err := n.lookup(ctx, kind, refID, key)
	if err != nil {
		return Fact{}, err
	}
	action := ActionWrite
	if exists && !prev.Deleted {
		action = ActionEdit
	}
	fact, err := applyHumanMutation(prev, exists, actor, kind, refID, key, body, source, action, false, now, NewFactID)
	if err != nil {
		return Fact{}, err
	}
	if err := n.saveFact(ctx, fact, exists); err != nil {
		return Fact{}, err
	}
	if err := n.record(ctx, "memory.write", fact.Key, fact.ID.String(), actor.Name); err != nil {
		return Fact{}, err
	}
	return fact, nil
}

// Delete tombstones a live fact. Actor must be human.
func (n *Notebook) Delete(ctx context.Context, actor Actor, kind, refID, key, source string) (Fact, error) {
	if err := requireHuman(actor); err != nil {
		return Fact{}, err
	}
	kind, refID, key, _, err := normalizeEntry(kind, refID, key, "tombstone")
	if err != nil {
		return Fact{}, err
	}
	prev, exists, err := n.lookup(ctx, kind, refID, key)
	if err != nil {
		return Fact{}, err
	}
	if !exists || prev.Deleted {
		return Fact{}, fmt.Errorf("memory delete %q: %w", key, ErrNotFound)
	}
	fact, err := applyHumanMutation(prev, true, actor, kind, refID, key, prev.Body, source, ActionDelete, true, n.now(), NewFactID)
	if err != nil {
		return Fact{}, err
	}
	if err := n.saveFact(ctx, fact, true); err != nil {
		return Fact{}, err
	}
	if err := n.record(ctx, "memory.delete", fact.Key, fact.ID.String(), actor.Name); err != nil {
		return Fact{}, err
	}
	return fact, nil
}

// Propose queues an agent-authored row. It does not create a fact.
func (n *Notebook) Propose(ctx context.Context, actor Actor, kind, refID, key, body, source string, sessionID store.SessionID) (Proposal, error) {
	if err := requireAgent(actor); err != nil {
		return Proposal{}, err
	}
	kind, refID, key, body, err := normalizeEntry(kind, refID, key, body)
	if err != nil {
		return Proposal{}, err
	}
	id, err := NewProposalID()
	if err != nil {
		return Proposal{}, err
	}
	p := Proposal{
		ID:        id,
		Kind:      kind,
		RefID:     refID,
		SessionID: sessionID,
		Key:       key,
		Body:      body,
		Agent:     actor,
		Status:    StatusPending,
		Source:    source,
		CreatedAt: n.now(),
	}
	if err := n.store.CreateMemoryProposal(ctx, storeFromProposal(p)); err != nil {
		return Proposal{}, fmt.Errorf("memory propose: %w", err)
	}
	if err := n.record(ctx, "memory.propose", p.Key, p.ID.String(), actor.Name); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

// Accept admits a pending proposal. Actor must be human.
func (n *Notebook) Accept(ctx context.Context, actor Actor, id store.MemoryProposalID) (Proposal, Fact, error) {
	if err := requireHuman(actor); err != nil {
		return Proposal{}, Fact{}, err
	}
	p, err := n.loadProposal(ctx, id)
	if err != nil {
		return Proposal{}, Fact{}, err
	}
	if p.Status != StatusPending {
		return Proposal{}, Fact{}, fmt.Errorf("memory accept: %w", ErrNotPending)
	}
	prev, exists, err := n.lookup(ctx, p.Kind, p.RefID, p.Key)
	if err != nil {
		return Proposal{}, Fact{}, err
	}
	action := ActionAccept
	if exists && !prev.Deleted {
		action = ActionEdit
	}
	source := p.ID.String()
	if p.Agent.Name != "" {
		source = p.ID.String() + " agent:" + p.Agent.Name
	}
	fact, err := applyHumanMutation(prev, exists, actor, p.Kind, p.RefID, p.Key, p.Body, source, action, false, n.now(), NewFactID)
	if err != nil {
		return Proposal{}, Fact{}, err
	}
	if err := n.saveFact(ctx, fact, exists); err != nil {
		return Proposal{}, Fact{}, err
	}
	p.Status = StatusAccepted
	p.MemoryID = fact.ID
	p.ReviewedAt = fact.UpdatedAt
	if err := n.store.UpdateMemoryProposal(ctx, storeFromProposal(p)); err != nil {
		return Proposal{}, Fact{}, fmt.Errorf("memory accept: %w", err)
	}
	if err := n.record(ctx, "memory.propose.accept", p.Key, p.ID.String(), actor.Name); err != nil {
		return Proposal{}, Fact{}, err
	}
	return p, fact, nil
}

// Reject records a human refusal. The proposal never becomes a fact.
func (n *Notebook) Reject(ctx context.Context, actor Actor, id store.MemoryProposalID) (Proposal, error) {
	if err := requireHuman(actor); err != nil {
		return Proposal{}, err
	}
	p, err := n.loadProposal(ctx, id)
	if err != nil {
		return Proposal{}, err
	}
	if p.Status != StatusPending {
		return Proposal{}, fmt.Errorf("memory reject: %w", ErrNotPending)
	}
	p.Status = StatusRejected
	p.ReviewedAt = n.now()
	if err := n.store.UpdateMemoryProposal(ctx, storeFromProposal(p)); err != nil {
		return Proposal{}, fmt.Errorf("memory reject: %w", err)
	}
	if err := n.record(ctx, "memory.propose.reject", p.Key, p.ID.String(), actor.Name); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

// Get loads one fact by scope and key.
func (n *Notebook) Get(ctx context.Context, kind, refID, key string) (Fact, error) {
	kind, refID, key, _, err := normalizeEntry(kind, refID, key, "x")
	if err != nil {
		return Fact{}, err
	}
	fact, ok, err := n.lookup(ctx, kind, refID, key)
	if err != nil {
		return Fact{}, err
	}
	if !ok {
		return Fact{}, fmt.Errorf("memory get %q: %w", key, ErrNotFound)
	}
	return fact, nil
}

// History returns every revision with its author.
func (n *Notebook) History(ctx context.Context, kind, refID, key string) ([]Revision, error) {
	fact, err := n.Get(ctx, kind, refID, key)
	if err != nil {
		return nil, err
	}
	return append([]Revision(nil), fact.Versions...), nil
}

// Slice returns live facts for compilation. kind/refID filter when set;
// empty kind returns every live row (paginated through the store).
func (n *Notebook) Slice(ctx context.Context, kind, refID string) ([]Fact, error) {
	entries, err := n.listAll(ctx, store.MemoryQuery{Kind: kind, RefID: refID})
	if err != nil {
		return nil, err
	}
	out := make([]Fact, 0, len(entries))
	for _, m := range entries {
		f := factFromEntry(m)
		if f.Deleted {
			continue
		}
		out = append(out, f)
	}
	sortFacts(out)
	return out, nil
}

// ListProposals returns proposal rows, newest first.
func (n *Notebook) ListProposals(ctx context.Context, status string) ([]Proposal, error) {
	q := store.MemoryProposalQuery{Status: status, PageQuery: store.PageQuery{Limit: store.MaxLimit}}
	var out []Proposal
	for {
		page, err := n.store.ListMemoryProposals(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("memory list proposals: %w", err)
		}
		for _, p := range page.Items {
			out = append(out, proposalFromStore(p))
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}
	return out, nil
}

// GetProposal loads one queued row.
func (n *Notebook) GetProposal(ctx context.Context, id store.MemoryProposalID) (Proposal, error) {
	return n.loadProposal(ctx, id)
}

func (n *Notebook) loadProposal(ctx context.Context, id store.MemoryProposalID) (Proposal, error) {
	if id.IsZero() {
		return Proposal{}, fmt.Errorf("memory proposal: %w", ErrInvalid)
	}
	p, err := n.store.GetMemoryProposal(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Proposal{}, fmt.Errorf("memory proposal: %w", ErrNotFound)
		}
		return Proposal{}, fmt.Errorf("memory proposal: %w", err)
	}
	return proposalFromStore(p), nil
}

func (n *Notebook) lookup(ctx context.Context, kind, refID, key string) (Fact, bool, error) {
	page, err := n.store.ListMemory(ctx, store.MemoryQuery{
		Kind:      kind,
		RefID:     refID,
		Key:       key,
		PageQuery: store.PageQuery{Limit: 8},
	})
	if err != nil {
		return Fact{}, false, fmt.Errorf("memory lookup: %w", err)
	}
	for _, m := range page.Items {
		if m.Key == key && m.Kind == kind && m.RefID == refID {
			return factFromEntry(m), true, nil
		}
		if m.Key == "" && m.ID.String() == key {
			return factFromEntry(m), true, nil
		}
	}
	return Fact{}, false, nil
}

func (n *Notebook) saveFact(ctx context.Context, fact Fact, exists bool) error {
	row := entryFromFact(fact)
	if exists {
		if err := n.store.UpdateMemory(ctx, row); err != nil {
			return fmt.Errorf("memory save: %w", err)
		}
		return nil
	}
	if err := n.store.CreateMemory(ctx, row); err != nil {
		return fmt.Errorf("memory save: %w", err)
	}
	return nil
}

func (n *Notebook) listAll(ctx context.Context, q store.MemoryQuery) ([]store.MemoryEntry, error) {
	q.Limit = store.MaxLimit
	var out []store.MemoryEntry
	for {
		page, err := n.store.ListMemory(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("memory list: %w", err)
		}
		out = append(out, page.Items...)
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}
	return out, nil
}

func (n *Notebook) record(ctx context.Context, action, target, resourceID, actor string) error {
	if n.audit == nil {
		return nil
	}
	if err := n.audit.Append(ctx, action, target, resourceID, actor); err != nil {
		return fmt.Errorf("memory audit %s: %w", action, err)
	}
	return nil
}
