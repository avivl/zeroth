package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
)

const applyPrincipal policy.PrincipalID = "operator"

func (s *Server) applyApproved(ctx context.Context, sess store.Session, p plan.Plan) (plan.Result, string, error) {
	world := newApplyWorld(p)
	world.server = s
	world.session = sess
	aud := &applyAuditor{log: s.audit, agent: sess.AgentID, session: sess.ID, planID: p.ID.String(), hash: string(p.Hash)}
	sid, err := session.ParseID(sess.ID.String())
	if err != nil {
		return plan.Result{}, "", fmt.Errorf("server apply: %w", err)
	}
	applier := &plan.Applier{
		Kernel:      policy.New(),
		World:       world,
		Leases:      &applyLeaser{store: s.store, agent: sess.AgentID},
		Checkpoints: &applyCheckpointer{server: s, id: sid},
		Audit:       aud,
	}
	result, err := applier.Apply(ctx, applyPrincipal, p, plan.Approval{PlanHash: p.Hash})
	if err != nil {
		return result, "", err
	}
	return result, aud.lastID, nil
}

type applyWorld struct {
	mu      sync.Mutex
	hashes  map[string]string
	applied map[string]string
	server  *Server
	session store.Session
}

func newApplyWorld(p plan.Plan) *applyWorld {
	hashes := make(map[string]string, len(p.Rows))
	for _, row := range p.Rows {
		hashes[row.Target] = row.Precondition
	}
	return &applyWorld{hashes: hashes, applied: make(map[string]string)}
}

func (w *applyWorld) Observe(_ context.Context, target string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hashes[target], nil
}

func (w *applyWorld) Execute(ctx context.Context, row plan.Row) (string, error) {
	w.mu.Lock()
	if post, ok := w.applied[row.IdempotencyKey]; ok {
		w.mu.Unlock()
		return post, nil
	}
	w.mu.Unlock()
	if row.Op == plan.OpMemoryProposal && w.server != nil {
		id, err := newMemoryProposalID()
		if err != nil {
			return "", fmt.Errorf("server apply memory: %w", err)
		}
		now := time.Now().UTC()
		if err := w.server.store.CreateMemoryProposal(ctx, store.MemoryProposal{
			ID:        id,
			Kind:      storeKindForMemory(row),
			RefID:     row.Target,
			SessionID: w.session.ID,
			Content:   row.Payload,
			Status:    "pending",
			CreatedAt: now,
		}); err != nil {
			return "", fmt.Errorf("server apply memory: %w", err)
		}
	}
	post := row.Postcondition
	if post == "" {
		post = "applied:" + row.IdempotencyKey
	}
	w.mu.Lock()
	w.hashes[row.Target] = post
	w.applied[row.IdempotencyKey] = post
	w.mu.Unlock()
	return post, nil
}

func (w *applyWorld) Seen(key string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	post, ok := w.applied[key]
	return post, ok
}

func storeKindForMemory(_ plan.Row) string {
	return "session"
}

type applyLeaser struct {
	store store.Store
	agent store.AgentID
}

func (l *applyLeaser) Acquire(ctx context.Context, p plan.Plan) ([]policy.Lease, error) {
	kinds := make([]policy.EffectKind, 0, 4)
	seenKind := make(map[policy.EffectKind]struct{})
	for _, row := range p.Rows {
		k := row.Op.Kind()
		if _, ok := seenKind[k]; ok {
			continue
		}
		seenKind[k] = struct{}{}
		kinds = append(kinds, k)
	}
	grant, err := newGrantID()
	if err != nil {
		return nil, fmt.Errorf("server apply lease: %w", err)
	}
	scope, err := store.ParseScopeID(string(p.Scope))
	if err != nil {
		return nil, fmt.Errorf("server apply lease: %w", err)
	}
	now := time.Now().UTC()
	seen := make(map[string]struct{})
	out := make([]policy.Lease, 0)
	for _, row := range p.Rows {
		id := string(row.Lease)
		if id == "" {
			return nil, fmt.Errorf("server apply lease: %w", plan.ErrInvalid)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		lid, err := store.ParseLeaseID(id)
		if err != nil {
			return nil, fmt.Errorf("server apply lease: %w", err)
		}
		if err := l.store.CreateLease(ctx, store.Lease{
			ID:        lid,
			GrantID:   grant,
			ScopeID:   scope,
			AgentID:   l.agent,
			ExpiresAt: p.ExpiresAt,
			MintedAt:  now,
		}); err != nil && !errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("server apply lease: %w", err)
		}
		out = append(out, policy.NewLease(policy.LeaseID(id), p.Scope, applyPrincipal, p.ExpiresAt, kinds...))
	}
	return out, nil
}

func (l *applyLeaser) Release(_ context.Context, _ []policy.Lease) error {
	return nil
}

type applyCheckpointer struct {
	server *Server
	id     session.ID
}

func (c *applyCheckpointer) Checkpoint(ctx context.Context) (plan.CheckpointRef, error) {
	ck, err := c.server.snapshotRun(ctx, c.id, "pre-apply")
	if err != nil {
		return "", err
	}
	return plan.CheckpointRef(ck.ID.String()), nil
}

type applyAuditor struct {
	log     *audit.Log
	agent   store.AgentID
	session store.SessionID
	planID  string
	hash    string
	lastID  string
}

func (a *applyAuditor) SignRow(context.Context, plan.Row, string) error { return nil }

func (a *applyAuditor) SignPlan(ctx context.Context, p plan.Plan) error {
	rec, err := a.log.Append(ctx, audit.Entry{
		Action:        audit.ActionPlanApply,
		Target:        a.planID,
		PlanHash:      a.hash,
		Postcondition: string(p.Status),
		Approver:      audit.ApproverOperator,
		AgentID:       a.agent,
		SessionID:     a.session,
		ResourceType:  "plan",
		ResourceID:    a.planID,
	})
	if err != nil {
		return err
	}
	a.lastID = rec.ID.String()
	return nil
}
