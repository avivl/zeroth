package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

// Book is the I/O-free notebook. Facts enter only through Write (human)
// or Accept (human accepting an agent proposal). There is no flag that
// lets an agent write directly.
type Book struct {
	mu        sync.Mutex
	facts     map[string]Fact
	proposals map[string]Proposal
	now       func() time.Time
	newFact   func() (store.MemoryID, error)
	newProp   func() (store.MemoryProposalID, error)
}

// NewBook returns an empty in-memory notebook.
func NewBook() *Book {
	return &Book{
		facts:     make(map[string]Fact),
		proposals: make(map[string]Proposal),
		now:       func() time.Time { return time.Now().UTC() },
		newFact:   NewFactID,
		newProp:   NewProposalID,
	}
}

// Write inserts or edits a fact. Actor must be human.
func (b *Book) Write(actor Actor, kind, refID, key, body, source string) (Fact, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if err := requireHuman(actor); err != nil {
		return Fact{}, err
	}
	kind, refID, key, body, err := normalizeEntry(kind, refID, key, body)
	if err != nil {
		return Fact{}, err
	}
	idx := scopeKey(kind, refID, key)
	prev, ok := b.facts[idx]
	action := ActionWrite
	if ok && !prev.Deleted {
		action = ActionEdit
	}
	fact, err := applyHumanMutation(prev, ok, actor, kind, refID, key, body, source, action, false, now, b.newFact)
	if err != nil {
		return Fact{}, err
	}
	b.facts[idx] = fact
	return cloneFact(fact), nil
}

// Delete tombstones a fact. Actor must be human. History is kept.
func (b *Book) Delete(actor Actor, kind, refID, key, source string) (Fact, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if err := requireHuman(actor); err != nil {
		return Fact{}, err
	}
	kind, refID, key, _, err := normalizeEntry(kind, refID, key, "tombstone")
	if err != nil {
		return Fact{}, err
	}
	idx := scopeKey(kind, refID, key)
	prev, ok := b.facts[idx]
	if !ok || prev.Deleted {
		return Fact{}, fmt.Errorf("memory delete %q: %w", key, ErrNotFound)
	}
	fact, err := applyHumanMutation(prev, true, actor, kind, refID, key, prev.Body, source, ActionDelete, true, now, b.newFact)
	if err != nil {
		return Fact{}, err
	}
	b.facts[idx] = fact
	return cloneFact(fact), nil
}

// Propose queues an agent write. It never creates a fact.
func (b *Book) Propose(actor Actor, kind, refID, key, body, source string, sessionID store.SessionID) (Proposal, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if err := requireAgent(actor); err != nil {
		return Proposal{}, err
	}
	kind, refID, key, body, err := normalizeEntry(kind, refID, key, body)
	if err != nil {
		return Proposal{}, err
	}
	id, err := b.newProp()
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
		CreatedAt: now,
	}
	b.proposals[id.String()] = p
	return p, nil
}

// Accept admits a pending proposal into the notebook. Actor must be human.
func (b *Book) Accept(actor Actor, id store.MemoryProposalID) (Proposal, Fact, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if err := requireHuman(actor); err != nil {
		return Proposal{}, Fact{}, err
	}
	if id.IsZero() {
		return Proposal{}, Fact{}, fmt.Errorf("memory accept: %w", ErrInvalid)
	}
	p, ok := b.proposals[id.String()]
	if !ok {
		return Proposal{}, Fact{}, fmt.Errorf("memory accept: %w", ErrNotFound)
	}
	if p.Status != StatusPending {
		return Proposal{}, Fact{}, fmt.Errorf("memory accept: %w", ErrNotPending)
	}
	idx := scopeKey(p.Kind, p.RefID, p.Key)
	prev, exists := b.facts[idx]
	action := ActionAccept
	if exists && !prev.Deleted {
		action = ActionEdit
	}
	source := p.ID.String()
	if p.Agent.Name != "" {
		source = p.ID.String() + " agent:" + p.Agent.Name
	}
	fact, err := applyHumanMutation(prev, exists, actor, p.Kind, p.RefID, p.Key, p.Body, source, action, false, now, b.newFact)
	if err != nil {
		return Proposal{}, Fact{}, err
	}
	p.Status = StatusAccepted
	p.MemoryID = fact.ID
	p.ReviewedAt = now
	b.facts[idx] = fact
	b.proposals[id.String()] = p
	return p, cloneFact(fact), nil
}

// Reject records a human refusal. The proposal never becomes a fact.
func (b *Book) Reject(actor Actor, id store.MemoryProposalID) (Proposal, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if err := requireHuman(actor); err != nil {
		return Proposal{}, err
	}
	if id.IsZero() {
		return Proposal{}, fmt.Errorf("memory reject: %w", ErrInvalid)
	}
	p, ok := b.proposals[id.String()]
	if !ok {
		return Proposal{}, fmt.Errorf("memory reject: %w", ErrNotFound)
	}
	if p.Status != StatusPending {
		return Proposal{}, fmt.Errorf("memory reject: %w", ErrNotPending)
	}
	p.Status = StatusRejected
	p.ReviewedAt = now
	b.proposals[id.String()] = p
	return p, nil
}

// Get returns the fact for key, including a tombstone.
func (b *Book) Get(kind, refID, key string) (Fact, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	kind, refID, key, _, err := normalizeEntry(kind, refID, key, "x")
	if err != nil {
		return Fact{}, err
	}
	fact, ok := b.facts[scopeKey(kind, refID, key)]
	if !ok {
		return Fact{}, fmt.Errorf("memory get %q: %w", key, ErrNotFound)
	}
	return cloneFact(fact), nil
}

// History returns every revision of a fact, oldest first, with authors.
func (b *Book) History(kind, refID, key string) ([]Revision, error) {
	fact, err := b.Get(kind, refID, key)
	if err != nil {
		return nil, err
	}
	return append([]Revision(nil), fact.Versions...), nil
}

// DiffView is the version-to-version view of a fact.
func (b *Book) DiffView(kind, refID, key string, from, to int) (Diff, error) {
	hist, err := b.History(kind, refID, key)
	if err != nil {
		return Diff{}, err
	}
	var a, c Revision
	var gotA, gotC bool
	for _, r := range hist {
		if r.Version == from {
			a, gotA = r, true
		}
		if r.Version == to {
			c, gotC = r, true
		}
	}
	if !gotA || !gotC {
		return Diff{}, fmt.Errorf("memory diff: %w", ErrNotFound)
	}
	return Diff{Key: key, From: a, To: c, Unified: unified(a.Body, c.Body)}, nil
}

// Slice returns live (not deleted) facts, sorted by key. This is what
// compilation reads.
func (b *Book) Slice() []Fact {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Fact, 0, len(b.facts))
	for _, f := range b.facts {
		if f.Deleted {
			continue
		}
		out = append(out, cloneFact(f))
	}
	sortFacts(out)
	return out
}

// Proposal returns one queued row.
func (b *Book) Proposal(id store.MemoryProposalID) (Proposal, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.proposals[id.String()]
	if !ok {
		return Proposal{}, fmt.Errorf("memory proposal: %w", ErrNotFound)
	}
	return p, nil
}

// Proposals returns queued rows, newest first. status filters when set.
func (b *Book) Proposals(status string) []Proposal {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Proposal, 0, len(b.proposals))
	for _, p := range b.proposals {
		if status != "" && p.Status != status {
			continue
		}
		out = append(out, p)
	}
	sortProposals(out)
	return out
}

func requireHuman(actor Actor) error {
	if !actor.valid() {
		return fmt.Errorf("memory actor: %w", ErrInvalid)
	}
	if !actor.IsHuman() {
		return ErrAgentCannotWrite
	}
	return nil
}

func requireAgent(actor Actor) error {
	if !actor.valid() {
		return fmt.Errorf("memory actor: %w", ErrInvalid)
	}
	if !actor.IsAgent() {
		return ErrHumanCannotPropose
	}
	return nil
}

func normalizeEntry(kind, refID, key, body string) (string, string, string, string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = KindOperator
	}
	switch kind {
	case KindOperator, KindSession, KindAgent:
	default:
		return "", "", "", "", fmt.Errorf("memory kind %q: %w", kind, ErrInvalid)
	}
	refID = strings.TrimSpace(refID)
	key = strings.TrimSpace(key)
	body = strings.TrimSpace(body)
	if key == "" || body == "" {
		return "", "", "", "", fmt.Errorf("memory entry: %w", ErrInvalid)
	}
	if kind != KindOperator && refID == "" {
		return "", "", "", "", fmt.Errorf("memory ref_id: %w", ErrInvalid)
	}
	return kind, refID, key, body, nil
}

func applyHumanMutation(
	prev Fact,
	exists bool,
	actor Actor,
	kind, refID, key, body, source, action string,
	deleted bool,
	now time.Time,
	newID func() (store.MemoryID, error),
) (Fact, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	fact := prev
	if !exists {
		id, err := newID()
		if err != nil {
			return Fact{}, err
		}
		fact = Fact{
			ID:        id,
			Kind:      kind,
			RefID:     refID,
			Key:       key,
			CreatedAt: now,
		}
	}
	ver := len(fact.Versions) + 1
	rev := Revision{
		Version: ver,
		Key:     key,
		Body:    body,
		Author:  actor,
		Action:  action,
		Source:  source,
		Deleted: deleted,
		At:      now,
	}
	fact.Key = key
	fact.Kind = kind
	fact.RefID = refID
	fact.Body = body
	fact.Deleted = deleted
	fact.UpdatedAt = now
	fact.Provenance = Provenance{Who: actor, What: action, When: now, Source: source}
	fact.Versions = append(append([]Revision(nil), fact.Versions...), rev)
	return fact, nil
}

func cloneFact(f Fact) Fact {
	out := f
	if f.Versions != nil {
		out.Versions = append([]Revision(nil), f.Versions...)
	}
	return out
}
