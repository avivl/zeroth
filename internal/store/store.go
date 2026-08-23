package store

import "context"

// Store is durable state for the control plane.
//
// A Store is a port. Concrete backends (SQLite, and later others) implement
// this interface; zerothd depends on the port, not the engine.
//
// The event log is the source of truth for sessions. Events and audit
// records are append-only: there is no update or delete on those methods.
type Store interface {
	// Name is a stable identifier used in logs, audit records, and
	// conformance tests (for example "sqlite").
	Name() string
	// Close releases backend resources. It is idempotent.
	Close() error

	CreateAgent(ctx context.Context, a Agent) error
	GetAgent(ctx context.Context, id AgentID) (Agent, error)
	UpdateAgent(ctx context.Context, a Agent) error
	ListAgents(ctx context.Context, q PageQuery) (Page[Agent], error)
	// CrossExamStats aggregates reviewer verdicts for plans whose
	// session belongs to this agent. A missing agent is ErrNotFound.
	CrossExamStats(ctx context.Context, agentID AgentID) (CrossExamStats, error)

	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, id SessionID) (Session, error)
	// UpdateSession replaces mutable session fields. A zero PlanID in s
	// does not clear a stored plan id: attaching a plan is monotonic.
	UpdateSession(ctx context.Context, s Session) error
	ListSessions(ctx context.Context, q SessionQuery) (Page[Session], error)

	// AppendEvent writes one event in its own transaction.
	AppendEvent(ctx context.Context, sessionID SessionID, ev Event) (Event, error)
	// AppendEvents writes events in a single transaction. Batching is the
	// G6 mitigation if one-row writes stall. Empty batches are invalid.
	AppendEvents(ctx context.Context, sessionID SessionID, events []Event) ([]Event, error)
	// ReplayLast returns the last n events in chronological order.
	ReplayLast(ctx context.Context, sessionID SessionID, n int) ([]Event, error)
	// EventsAfter returns events with Seq greater than afterSeq, chronological.
	EventsAfter(ctx context.Context, sessionID SessionID, afterSeq int64) ([]Event, error)

	CreatePlan(ctx context.Context, p Plan) error
	GetPlan(ctx context.Context, id PlanID) (Plan, error)
	UpdatePlan(ctx context.Context, p Plan) error
	ListPlans(ctx context.Context, q PlanQuery) (Page[Plan], error)

	CreateApproval(ctx context.Context, a Approval) error
	GetApproval(ctx context.Context, id ApprovalID) (Approval, error)
	UpdateApproval(ctx context.Context, a Approval) error
	ListApprovals(ctx context.Context, q ApprovalQuery) (Page[Approval], error)

	CreateMemory(ctx context.Context, m MemoryEntry) error
	GetMemory(ctx context.Context, id MemoryID) (MemoryEntry, error)
	UpdateMemory(ctx context.Context, m MemoryEntry) error
	ListMemory(ctx context.Context, q MemoryQuery) (Page[MemoryEntry], error)

	CreateMemoryProposal(ctx context.Context, p MemoryProposal) error
	GetMemoryProposal(ctx context.Context, id MemoryProposalID) (MemoryProposal, error)
	UpdateMemoryProposal(ctx context.Context, p MemoryProposal) error
	ListMemoryProposals(ctx context.Context, q MemoryProposalQuery) (Page[MemoryProposal], error)

	AppendAudit(ctx context.Context, r AuditRecord) (AuditRecord, error)
	GetAudit(ctx context.Context, id AuditID) (AuditRecord, error)
	ListAudit(ctx context.Context, q AuditQuery) (Page[AuditRecord], error)
	// AuditChain returns every audit record oldest-first. Verify walks this.
	AuditChain(ctx context.Context) ([]AuditRecord, error)

	AppendAgentKey(ctx context.Context, k AgentKey) error
	// ListAgentKeys returns pubkeys oldest-first. A zero agentID lists every
	// registered key so offline verify can load the registry in one call.
	ListAgentKeys(ctx context.Context, agentID AgentID) ([]AgentKey, error)

	CreateLease(ctx context.Context, l Lease) error
	GetLease(ctx context.Context, id LeaseID) (Lease, error)
	UpdateLease(ctx context.Context, l Lease) error
	ListLeases(ctx context.Context, q LeaseQuery) (Page[Lease], error)
	// DeleteLease removes a lease so it cannot authorize further
	// effects. A missing id is ErrNotFound. Empty id is ErrInvalid.
	DeleteLease(ctx context.Context, id LeaseID) error

	CreateCheckpoint(ctx context.Context, c Checkpoint) error
	GetCheckpoint(ctx context.Context, id CheckpointID) (Checkpoint, error)
	ListCheckpoints(ctx context.Context, q CheckpointQuery) (Page[Checkpoint], error)
}
