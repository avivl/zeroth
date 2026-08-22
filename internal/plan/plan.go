package plan

import (
	"time"

	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/session"
)

// Status is the plan lifecycle. Apply is the only world-changing value
// sequence: approved (then applying). Drafts are not executable.
type Status string

const (
	StatusDraft            Status = "draft"
	StatusCrossExam        Status = "cross_exam"
	StatusPendingApproval  Status = "pending_approval"
	StatusApproved         Status = "approved"
	StatusChangesRequested Status = "changes_requested"
	StatusApplying         Status = "applying"
	StatusApplied          Status = "applied"
	StatusAbandoned        Status = "abandoned"
	// StatusStale is an apply outcome: a precondition drifted, nothing
	// was written, and the agent must re-draft. Not an OpenAPI PlanStatus.
	StatusStale Status = "stale"
	// StatusPartiallyApplied is an apply outcome: a prefix of rows
	// landed and the rest did not. Recovery re-drafts from the recorded
	// boundary. Not an OpenAPI PlanStatus.
	StatusPartiallyApplied Status = "partially_applied"
)

// Credential names a credential class the plan was drafted under.
// Provider and Kind are labels (for example "anthropic" and "api_key").
// The secret itself never belongs here (ADR-Z-0008).
type Credential struct {
	Provider string
	Kind     string
}

// CrossExam is the automatic challenge of a draft. It is not part of the
// canonical hash: a reviewer verdict does not rewrite what was approved.
type CrossExam struct {
	Verdict       string
	ReviewerModel string
	Reasoning     string
	At            time.Time
}

// Finding names a secretscan hit without the secret itself.
type Finding struct {
	Path string
	Rule string
	Line int
}

// Plan is the product: typed rows plus the constraints they were drafted
// under, identified by Hash. Identity, status, and review metadata persist
// but are not hashed, so approve does not itself produce a new plan.
type Plan struct {
	ID            ID
	SessionID     session.ID
	ParentID      ID
	Status        Status
	Summary       string
	Hash          policy.PlanHash
	ExpiresAt     time.Time
	CostCeiling   int64
	Scope         policy.ScopeID
	Credentials   []Credential
	Rows          []Row
	CrossExam     *CrossExam
	Findings      []Finding
	ReviewComment string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PolicyPlan is what the kernel sees: the canonical hash and one effect
// per row. Apply still has to check the full row (payload, hashes, lease)
// in this package; the kernel only answers whether that effect is in the
// approved set and covered by a lease.
func (p Plan) PolicyPlan() policy.Plan {
	effects := make([]policy.Effect, 0, len(p.Rows))
	for _, r := range p.Rows {
		effects = append(effects, r.PolicyEffect(p.Scope))
	}
	return policy.Plan{Hash: p.Hash, Effects: effects}
}

// Approve returns a copy marked approved. It does not change Hash: the
// operator is gating this exact bundle, not revising it. Expired or
// hash-mismatched plans are rejected.
func (p Plan) Approve(now time.Time) (Plan, error) {
	if err := p.checkBundle(now); err != nil {
		return Plan{}, err
	}
	switch p.Status {
	case StatusDraft, StatusCrossExam, StatusPendingApproval, StatusChangesRequested:
	default:
		return Plan{}, fmtStatus("plan approve", p.Status)
	}
	out := p
	out.Status = StatusApproved
	out.UpdatedAt = now.UTC()
	return out, nil
}

func fmtStatus(op string, st Status) error {
	return &StatusError{Op: op, Status: st}
}
