package store

import (
	"fmt"
	"strings"
)

// ID is an opaque identifier of kind K. Distinct K values produce types that
// are not interchangeable (ADR-Z-0001). They are not string or int64.
type ID[K any] struct {
	raw string
}

func parseID[K any](kind, raw string) (ID[K], error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ID[K]{}, fmt.Errorf("store %s id: empty", kind)
	}
	return ID[K]{raw: raw}, nil
}

// String returns the raw identifier for logs, SQL, and the API. It is not
// for mixing with other ID kinds.
func (id ID[K]) String() string { return id.raw }

// IsZero reports whether id is the zero value.
func (id ID[K]) IsZero() bool { return id.raw == "" }

// Kind tags so SessionID and PlanID (and the rest) cannot be assigned to
// each other. The structs are empty; they exist only as type parameters.
type (
	sessionKind        struct{}
	eventKind          struct{}
	planKind           struct{}
	approvalKind       struct{}
	memoryKind         struct{}
	memoryProposalKind struct{}
	auditKind          struct{}
	leaseKind          struct{}
	checkpointKind     struct{}
	agentKind          struct{}
	grantKind          struct{}
	scopeKind          struct{}
)

// Persistence identifiers. These are distinct named types, not aliases of
// each other. Kernel packages may wrap them later; the store port uses these.
type (
	SessionID        = ID[sessionKind]
	EventID          = ID[eventKind]
	PlanID           = ID[planKind]
	ApprovalID       = ID[approvalKind]
	MemoryID         = ID[memoryKind]
	MemoryProposalID = ID[memoryProposalKind]
	AuditID          = ID[auditKind]
	LeaseID          = ID[leaseKind]
	CheckpointID     = ID[checkpointKind]
	AgentID          = ID[agentKind]
	GrantID          = ID[grantKind]
	ScopeID          = ID[scopeKind]
)

// Parse helpers reject empty identifiers.

func ParseSessionID(raw string) (SessionID, error) {
	return parseID[sessionKind]("session", raw)
}
func ParseEventID(raw string) (EventID, error) {
	return parseID[eventKind]("event", raw)
}
func ParsePlanID(raw string) (PlanID, error) {
	return parseID[planKind]("plan", raw)
}
func ParseApprovalID(raw string) (ApprovalID, error) {
	return parseID[approvalKind]("approval", raw)
}
func ParseMemoryID(raw string) (MemoryID, error) {
	return parseID[memoryKind]("memory", raw)
}
func ParseMemoryProposalID(raw string) (MemoryProposalID, error) {
	return parseID[memoryProposalKind]("memory proposal", raw)
}
func ParseAuditID(raw string) (AuditID, error) {
	return parseID[auditKind]("audit", raw)
}
func ParseLeaseID(raw string) (LeaseID, error) {
	return parseID[leaseKind]("lease", raw)
}
func ParseCheckpointID(raw string) (CheckpointID, error) {
	return parseID[checkpointKind]("checkpoint", raw)
}
func ParseAgentID(raw string) (AgentID, error) {
	return parseID[agentKind]("agent", raw)
}
func ParseGrantID(raw string) (GrantID, error) {
	return parseID[grantKind]("grant", raw)
}
func ParseScopeID(raw string) (ScopeID, error) {
	return parseID[scopeKind]("scope", raw)
}
