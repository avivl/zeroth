package memory

import (
	"time"

	"github.com/avivl/zeroth/internal/store"
)

const (
	ActionWrite  = "write"
	ActionEdit   = "edit"
	ActionAccept = "accept"
	ActionDelete = "delete"
	ActionReject = "reject"

	StatusPending  = "pending"
	StatusAccepted = "accepted"
	StatusRejected = "rejected"

	KindOperator = "operator"
	KindSession  = "session"
	KindAgent    = "agent"
)

// Provenance is who/what/when/source for one notebook mutation.
type Provenance struct {
	Who    Actor
	What   string
	When   time.Time
	Source string
}

// Revision is one version of a fact. History is append-only.
type Revision struct {
	Version int
	Key     string
	Body    string
	Author  Actor
	Action  string
	Source  string
	Deleted bool
	At      time.Time
}

// Fact is one atomic notebook entry. Deleted facts stay for history
// and drop out of compilation.
type Fact struct {
	ID         store.MemoryID
	Kind       string
	RefID      string
	Key        string
	Body       string
	Deleted    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Provenance Provenance
	Versions   []Revision
}

// Proposal is an agent-authored row waiting for human review. It is not
// a notebook fact until Accept.
type Proposal struct {
	ID         store.MemoryProposalID
	Kind       string
	RefID      string
	SessionID  store.SessionID
	Key        string
	Body       string
	Agent      Actor
	Status     string
	MemoryID   store.MemoryID
	Source     string
	CreatedAt  time.Time
	ReviewedAt time.Time
}

// Diff is a version-to-version view of one fact.
type Diff struct {
	Key     string
	From    Revision
	To      Revision
	Unified string
}

func scopeKey(kind, ref, key string) string {
	return kind + "\x00" + ref + "\x00" + key
}
