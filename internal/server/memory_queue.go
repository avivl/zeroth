package server

import (
	"context"
	"fmt"

	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/store"
)

// memoryQueue is the apply-time adapter: a memory_proposal row becomes
// Notebook.Propose. There is no Accept here (Z1-022).
type memoryQueue struct {
	nb      *memory.Notebook
	actor   memory.Actor
	session store.SessionID
	agentID string
}

func (s *Server) memoryQueue(sess store.Session) plan.Memory {
	nb := s.notebook()
	if nb == nil {
		return nil
	}
	name := sess.AgentID.String()
	if name == "" {
		name = DefaultAgentID
	}
	return memoryQueue{
		nb:      nb,
		actor:   memory.AgentActor(name),
		session: sess.ID,
		agentID: sess.AgentID.String(),
	}
}

func (q memoryQueue) Propose(ctx context.Context, row plan.Row) (string, error) {
	if q.nb == nil {
		return "", fmt.Errorf("plan apply memory: notebook unavailable")
	}
	kind, refID, key := memory.ParseProposalTarget(row.Target, q.session.String(), q.agentID)
	p, err := q.nb.Propose(ctx, q.actor, kind, refID, key, row.Payload, "plan.apply", q.session)
	if err != nil {
		return "", fmt.Errorf("plan apply memory propose: %w", err)
	}
	if row.Postcondition != "" {
		return row.Postcondition, nil
	}
	return p.ID.String(), nil
}
