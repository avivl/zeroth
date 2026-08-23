package plan

import (
	"fmt"
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

// Known cross-exam verdicts. The wire format is an open string; these are
// the values this package produces. Clients must tolerate unknowns.
const (
	VerdictPass          = "pass"
	VerdictFail          = "fail"
	VerdictPassWithNotes = "pass_with_notes"
)

// CrossExam is the automatic challenge of a draft. It is not part of the
// canonical hash: a reviewer verdict does not rewrite what was approved.
// Reasoning is the free-text notes shown inline. Empty notes on a
// nontrivial plan are themselves a signal.
type CrossExam struct {
	Verdict       string
	ReviewerModel string
	Reasoning     string
	At            time.Time
}

// FlagsConcern reports whether the verdict should be called out to a
// human before they approve. Fail and pass_with_notes are concerns.
// A silent pass is not.
func (e CrossExam) FlagsConcern() bool {
	switch e.Verdict {
	case VerdictFail, VerdictPassWithNotes:
		return true
	default:
		return false
	}
}

// Nontrivial reports whether p is large enough that a silent pass (a
// verdict with no notes) is worth tracking. One tiny row is not.
func (p Plan) Nontrivial() bool {
	if len(p.Rows) > 1 {
		return true
	}
	for _, r := range p.Rows {
		if len(r.Payload) > 80 {
			return true
		}
	}
	return false
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
	// AppliedThrough is the exclusive count of leading rows that landed
	// during apply. It is an outcome, not part of the approved bundle, so
	// it is not hashed. Recovery reads this rather than guessing.
	AppliedThrough int
	// Checkpoint is the pre-apply snapshot locator, when apply took one.
	// Unhashed for the same reason as AppliedThrough.
	Checkpoint CheckpointRef
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
// hash-mismatched plans are rejected. Drafts, in-flight exams, and
// block-on-fail returns are not the human gate: only a completed
// cross-exam that escalated (pending_approval) can be approved.
func (p Plan) Approve(now time.Time) (Plan, error) {
	if err := p.checkBundle(now); err != nil {
		return Plan{}, err
	}
	if p.CrossExam == nil {
		return Plan{}, fmt.Errorf("plan approve: %w", ErrNotExamined)
	}
	if p.Status != StatusPendingApproval {
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
