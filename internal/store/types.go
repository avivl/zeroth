package store

import "time"

// Session is one human-supervised run. The API calls this a run.
type Session struct {
	ID            SessionID
	AgentID       AgentID
	PlanID        PlanID
	Status        string
	Prompt        string
	TrackerRef    string
	Workspace     WorkspaceSource
	AutonomyTier  string
	PullRequest   string
	RetractReason string
	// HarnessSession is the vendor session id of the run's harness turn.
	// It is persisted so a daemon restart can resume the turn rather than
	// start it over from the prompt (42-78).
	HarnessSession string
	RetractedAt    time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	FinishedAt     time.Time
}

// WorkspaceSource is the repo a session checks out.
type WorkspaceSource struct {
	Repo string
	Ref  string
}

// SessionQuery filters ListSessions. Newest first.
type SessionQuery struct {
	PageQuery
	Status  string
	AgentID AgentID
}

// Event is one append-only log row. Seq is the order key and the source of
// truth for a session. The WebSocket stream is a live tail of this log.
type Event struct {
	Seq       int64
	ID        EventID
	SessionID SessionID
	Type      string
	PlanID    PlanID
	Message   string
	Payload   string
	CreatedAt time.Time
}

// Plan is a draft that must be cross-examined and approved before apply.
// Hash, ExpiresAt, CostCeiling, ScopeID, and Credentials are the plan-level
// fields from Z1-052. Hash is the canonical digest of the rows and those
// constraints; a mismatch with the recomputed digest is a revision.
type Plan struct {
	ID                 PlanID
	SessionID          SessionID
	ParentPlanID       PlanID
	Status             string
	Summary            string
	Hash               string
	ExpiresAt          time.Time
	CostCeiling        int64
	ScopeID            ScopeID
	Credentials        []CredentialConstraint
	Effects            []PlanEffect
	CrossExam          *CrossExam
	SecretScanFindings []SecretScanFinding
	ReviewComment      string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// CredentialConstraint names a credential class a plan was drafted under.
// Provider and Kind are labels. The secret itself is never stored.
type CredentialConstraint struct {
	Provider string
	Kind     string
}

// PlanEffect is one proposed mutation. The payload never includes a secret.
type PlanEffect struct {
	Type              string
	Path              string
	Diff              string
	PreconditionHash  string
	PostconditionHash string
	IdempotencyKey    string
	LeaseID           LeaseID
	CostEstimate      string
}

// CrossExam is the automatic challenge of a draft.
type CrossExam struct {
	Verdict       string
	ReviewerModel string
	Reasoning     string
	At            time.Time
}

// SecretScanFinding names a detector hit without the secret itself.
type SecretScanFinding struct {
	Path string
	Rule string
	Line int
}

// PlanQuery filters ListPlans. Newest first.
type PlanQuery struct {
	PageQuery
	SessionID SessionID
	Status    string
}

// Approval is an operator-inbox item. Decisions happen on the subject
// resource, not here.
type Approval struct {
	ID        ApprovalID
	Kind      string
	Status    string
	PlanID    PlanID
	SessionID SessionID
	Summary   string
	CreatedAt time.Time
}

// ApprovalQuery filters ListApprovals. Oldest pending first when Status is
// pending; otherwise newest first so the inbox and history share one method.
type ApprovalQuery struct {
	PageQuery
	Status string
}

// MemoryEntry is store-backed session, agent, or operator memory.
// Key, provenance, version history, and the deleted tombstone are the
// notebook fields (Z1-022). Content is the current body.
type MemoryEntry struct {
	ID         MemoryID
	Kind       string
	RefID      string
	Key        string
	Content    string
	Author     string
	AuthorKind string
	Source     string
	Action     string
	Deleted    bool
	Version    int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	History    []MemoryRevision
}

// MemoryRevision is one version of a notebook fact.
type MemoryRevision struct {
	Version    int
	Key        string
	Body       string
	Author     string
	AuthorKind string
	Action     string
	Source     string
	Deleted    bool
	At         time.Time
}

// MemoryQuery filters ListMemory. Newest first.
type MemoryQuery struct {
	PageQuery
	Kind  string
	RefID string
	Key   string
}

// MemoryProposal is a harness-proposed memory row awaiting human review.
type MemoryProposal struct {
	ID         MemoryProposalID
	Kind       string
	RefID      string
	SessionID  SessionID
	Key        string
	Content    string
	Author     string
	AuthorKind string
	Source     string
	Status     string
	MemoryID   MemoryID
	CreatedAt  time.Time
	ReviewedAt time.Time
}

// MemoryProposalQuery filters ListMemoryProposals. Newest first.
type MemoryProposalQuery struct {
	PageQuery
	Status string
}

// AuditRecord is one signed, append-only trail row. The signed payload is
// the issue's record shape (action, target, plan hash, pre/post, lease,
// approver, agent pubkey, prev_hash, ts). Hash is SHA-256 of that payload
// plus the signature, and is what the next row's PrevHash must match.
type AuditRecord struct {
	ID            AuditID
	Action        string
	Target        string
	PlanHash      string
	Precondition  string
	Postcondition string
	LeaseID       LeaseID
	Approver      string
	AgentPubKey   string
	PrevHash      string
	Hash          string
	Signature     string
	AgentID       AgentID
	SessionID     SessionID
	ResourceType  string
	ResourceID    string
	Actor         string
	CreatedAt     time.Time
}

// AuditQuery filters ListAudit. Newest first.
type AuditQuery struct {
	PageQuery
	ResourceType string
	ResourceID   string
	SessionID    SessionID
}

// AgentKey is one row in the append-only pubkey registry. Rotation inserts
// a new row; historical signatures keep verifying against earlier keys.
type AgentKey struct {
	AgentID   AgentID
	PubKey    string
	CreatedAt time.Time
}

// Lease is a time-bounded grant. Policy defines what a lease may be; the
// store only persists the facts the runtime passes in.
type Lease struct {
	ID        LeaseID
	GrantID   GrantID
	ScopeID   ScopeID
	AgentID   AgentID
	ExpiresAt time.Time
	MintedAt  time.Time
}

// LeaseQuery filters ListLeases. Newest minted first.
type LeaseQuery struct {
	PageQuery
	AgentID AgentID
}

// Checkpoint is an index row for a workspace snapshot. The blob lives
// outside the store (sandbox tar). Location is a backend-specific locator.
type Checkpoint struct {
	ID        CheckpointID
	SessionID SessionID
	Label     string
	Location  string
	CreatedAt time.Time
}

// CheckpointQuery filters ListCheckpoints. Newest first.
type CheckpointQuery struct {
	PageQuery
	SessionID SessionID
}

// Agent is a local agent record. Config changes are audited by callers.
type Agent struct {
	ID             AgentID
	Name           string
	Harness        string
	Status         string
	Model          string
	Tools          []string
	AutonomyTier   string
	ReviewerModel  string
	ReviewerModel2 string
	ReviewerDual   bool
	BlockOnFail    bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CrossExamStats is the per-agent reviewer scoreboard. PassRate is
// (Pass + PassWithNotes) / Examined, or 0 when nothing has been examined.
// EmptyNotesNontrivial is the silent-pass signal: a reviewer that always
// passes a nontrivial plan with zero notes is not reviewing.
type CrossExamStats struct {
	AgentID              AgentID
	Examined             int
	Pass                 int
	Fail                 int
	PassWithNotes        int
	EmptyNotesNontrivial int
}

// PassRate is (pass + pass_with_notes) / examined. Zero when examined is 0.
func (s CrossExamStats) PassRate() float64 {
	if s.Examined == 0 {
		return 0
	}
	return float64(s.Pass+s.PassWithNotes) / float64(s.Examined)
}
