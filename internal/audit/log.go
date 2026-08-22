package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/signer"
	"github.com/avivl/zeroth/internal/store"
)

// Entry is an unsigned action the log will sign and append.
type Entry struct {
	Action        string
	Target        string
	PlanHash      string
	Precondition  string
	Postcondition string
	LeaseID       store.LeaseID
	Approver      string
	AgentID       store.AgentID
	SessionID     store.SessionID
	ResourceType  string
	ResourceID    string
}

// Log signs and appends audit records onto a store. One Log per process;
// append is serialized so prev_hash stays a single chain.
type Log struct {
	store  store.Store
	signer signer.Service
	mu     sync.Mutex
}

// NewLog wires a store and a signer. Neither may be nil.
func NewLog(st store.Store, sg signer.Service) (*Log, error) {
	if st == nil {
		return nil, fmt.Errorf("audit log: nil store")
	}
	if sg == nil {
		return nil, fmt.Errorf("audit log: nil signer")
	}
	return &Log{store: st, signer: sg}, nil
}

// EnsureAgentKey creates a keypair and registry row if the agent has none.
// The first key is self-attested by an agent.create (or agent.enroll) record
// signed under that key.
func (l *Log) EnsureAgentKey(ctx context.Context, agentID store.AgentID, created bool) error {
	if agentID.IsZero() {
		return fmt.Errorf("audit ensure agent key: %w", store.ErrInvalid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	keys, err := l.store.ListAgentKeys(ctx, agentID)
	if err != nil {
		return wrap("ensure agent key", err)
	}
	if len(keys) > 0 {
		return nil
	}
	pub, err := l.signer.Create(ctx, agentID.String())
	if err != nil {
		if errors.Is(err, signer.ErrExists) {
			pub, err = l.signer.PublicKey(ctx, agentID.String())
			if err != nil {
				return wrap("ensure agent key", err)
			}
		} else {
			return wrap("ensure agent key", err)
		}
	}
	now := time.Now().UTC()
	if err := l.store.AppendAgentKey(ctx, store.AgentKey{
		AgentID:   agentID,
		PubKey:    pub.Hex(),
		CreatedAt: now,
	}); err != nil {
		return wrap("ensure agent key", err)
	}
	action := ActionAgentCreate
	if !created {
		action = "agent.enroll_key"
	}
	_, err = l.appendLocked(ctx, Entry{
		Action:       action,
		Target:       agentID.String(),
		Approver:     ApproverOperator,
		AgentID:      agentID,
		ResourceType: "agent",
		ResourceID:   agentID.String(),
	})
	return err
}

// RotateAgentKey mints a new key, signs the rotation with the old key, then
// appends the new pubkey to the registry. Historical signatures stay valid.
func (l *Log) RotateAgentKey(ctx context.Context, agentID store.AgentID) (store.AuditRecord, error) {
	if agentID.IsZero() {
		return store.AuditRecord{}, fmt.Errorf("audit rotate: %w", store.ErrInvalid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, err := l.appendLocked(ctx, Entry{
		Action:       ActionAgentRotateKey,
		Target:       agentID.String(),
		Approver:     ApproverOperator,
		AgentID:      agentID,
		ResourceType: "agent",
		ResourceID:   agentID.String(),
	})
	if err != nil {
		return store.AuditRecord{}, err
	}
	newPub, err := l.signer.Rotate(ctx, agentID.String())
	if err != nil {
		return store.AuditRecord{}, wrap("rotate", err)
	}
	if err := l.store.AppendAgentKey(ctx, store.AgentKey{
		AgentID:   agentID,
		PubKey:    newPub.Hex(),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return store.AuditRecord{}, wrap("rotate", err)
	}
	return rec, nil
}

// Append signs entry under the agent's current key and links it onto the chain.
func (l *Log) Append(ctx context.Context, entry Entry) (store.AuditRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.appendLocked(ctx, entry)
}

func (l *Log) appendLocked(ctx context.Context, entry Entry) (store.AuditRecord, error) {
	if entry.Action == "" || entry.ResourceType == "" || entry.ResourceID == "" || entry.AgentID.IsZero() {
		return store.AuditRecord{}, fmt.Errorf("audit append: %w", store.ErrInvalid)
	}
	if entry.Target == "" {
		entry.Target = entry.ResourceID
	}
	if entry.Approver == "" {
		entry.Approver = ApproverOperator
	}
	pub, err := l.signer.PublicKey(ctx, entry.AgentID.String())
	if err != nil {
		return store.AuditRecord{}, wrap("append", err)
	}
	prev, err := l.lastHash(ctx)
	if err != nil {
		return store.AuditRecord{}, err
	}
	now := time.Now().UTC()
	p := Payload{
		Action:        entry.Action,
		Target:        entry.Target,
		PlanHash:      entry.PlanHash,
		Precondition:  entry.Precondition,
		Postcondition: entry.Postcondition,
		LeaseID:       entry.LeaseID.String(),
		Approver:      entry.Approver,
		AgentPubKey:   pub.Hex(),
		PrevHash:      prev,
		Timestamp:     now,
	}
	dig := Digest(p)
	sig, err := l.signer.Sign(ctx, entry.AgentID.String(), dig[:])
	if err != nil {
		return store.AuditRecord{}, wrap("append", err)
	}
	id, err := NewID()
	if err != nil {
		return store.AuditRecord{}, wrap("append", err)
	}
	rec := store.AuditRecord{
		ID:            id,
		Action:        entry.Action,
		Target:        entry.Target,
		PlanHash:      entry.PlanHash,
		Precondition:  entry.Precondition,
		Postcondition: entry.Postcondition,
		LeaseID:       entry.LeaseID,
		Approver:      entry.Approver,
		AgentPubKey:   pub.Hex(),
		PrevHash:      prev,
		Hash:          ChainHash(p, sig),
		Signature:     sig.Hex(),
		AgentID:       entry.AgentID,
		SessionID:     entry.SessionID,
		ResourceType:  entry.ResourceType,
		ResourceID:    entry.ResourceID,
		Actor:         entry.Approver,
		CreatedAt:     now,
	}
	got, err := l.store.AppendAudit(ctx, rec)
	if err != nil {
		return store.AuditRecord{}, wrap("append", err)
	}
	return got, nil
}

func (l *Log) lastHash(ctx context.Context) (string, error) {
	page, err := l.store.ListAudit(ctx, store.AuditQuery{PageQuery: store.PageQuery{Limit: 1}})
	if err != nil {
		return "", wrap("last", err)
	}
	if len(page.Items) == 0 {
		return "", nil
	}
	return page.Items[0].Hash, nil
}

// NewID returns a random audit record id.
func NewID() (store.AuditID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return store.AuditID{}, fmt.Errorf("audit id: %w", err)
	}
	return store.ParseAuditID("aud_" + hex.EncodeToString(b[:]))
}
