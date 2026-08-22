package plan

import (
	"context"
	"fmt"
	"time"

	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/secretscan"
)

// CanApply reports whether apply may execute row as part of p at now.
// The effect set of an approved, unexpired, untampered plan is exactly
// the set of rows apply is permitted to do. Anything else is a deny.
func (p Plan) CanApply(row Row, now time.Time) error {
	if err := p.checkBundle(now); err != nil {
		return err
	}
	if p.Status != StatusApproved && p.Status != StatusApplying {
		return fmt.Errorf("plan apply: %w", ErrNotApproved)
	}
	if !p.contains(row) {
		return fmt.Errorf("plan apply: %w", ErrNotInPlan)
	}
	return nil
}

// Permitted returns the rows apply may execute at now. On a plan that is
// not yet approved, is expired, or has been revised, it returns an error
// and no rows: apply does not get a partial set.
func (p Plan) Permitted(now time.Time) ([]Row, error) {
	if err := p.checkBundle(now); err != nil {
		return nil, err
	}
	if p.Status != StatusApproved && p.Status != StatusApplying {
		return nil, fmt.Errorf("plan apply: %w", ErrNotApproved)
	}
	out := make([]Row, len(p.Rows))
	copy(out, p.Rows)
	return out, nil
}

func (p Plan) checkBundle(now time.Time) error {
	if p.Hash == "" || p.Hash != HashOf(p) {
		return fmt.Errorf("plan apply: %w", ErrHashMismatch)
	}
	if p.ExpiresAt.IsZero() || !now.Before(p.ExpiresAt) {
		return fmt.Errorf("plan apply: %w", ErrExpired)
	}
	return nil
}

func (p Plan) contains(row Row) bool {
	for _, r := range p.Rows {
		if r.equal(row) {
			return true
		}
	}
	return false
}

// CheckpointRef is the locator of a pre-apply snapshot. It is a distinct
// named type, not a Plan ID.
type CheckpointRef string

// Approval is the operator gate for one plan hash. Approving rev2 does
// not authorize rev3.
type Approval struct {
	PlanHash policy.PlanHash
}

// Applied is the recorded postcondition of one executed row. Index is
// the position in Plan.Rows. Recovery reads these rather than guessing.
type Applied struct {
	Key           string
	Index         int
	Postcondition string
}

// Result is the outcome of one Apply call. Plan is the updated copy the
// caller should persist. AppliedThrough is the count of leading rows
// that landed (the exclusive boundary). On a full success it equals
// len(Rows). On a fail-closed refuse it is 0.
type Result struct {
	Plan           Plan
	Status         Status
	AppliedThrough int
	Applied        []Applied
	Checkpoint     CheckpointRef
	Reason         string
}

func resultFrom(p Plan, applied []Applied, through int, ck CheckpointRef) Result {
	return Result{
		Plan:           p,
		Status:         p.Status,
		AppliedThrough: through,
		Applied:        applied,
		Checkpoint:     ck,
		Reason:         p.ReviewComment,
	}
}

// Executor applies rows and reports the live world. Observe must not
// write. Execute is idempotent by Row.IdempotencyKey. Seen lets a retry
// skip precondition checks for keys that already landed, so the
// postcondition is not mistaken for drift.
type Executor interface {
	Observe(ctx context.Context, target string) (string, error)
	Execute(ctx context.Context, row Row) (postcondition string, err error)
	Seen(key string) (postcondition string, ok bool)
}

// Checkpointer takes the pre-apply snapshot (workspace tar plus
// transcript in a real wiring). Apply treats a failure here as
// fail-closed: no rows run.
type Checkpointer interface {
	Checkpoint(ctx context.Context) (CheckpointRef, error)
}

// Leaser mints the leases every row must execute under, and releases
// them on the way out. Policy decides whether a lease still covers a
// row; this port only holds them.
type Leaser interface {
	Acquire(ctx context.Context, p Plan) ([]policy.Lease, error)
	Release(ctx context.Context, leases []policy.Lease) error
}

// Auditor signs each applied row and the plan as a whole. The real
// implementation lives in signer/audit (Linear 42-27); Apply only calls
// the port so the sequence stays complete.
type Auditor interface {
	SignRow(ctx context.Context, row Row, postcondition string) error
	SignPlan(ctx context.Context, p Plan) error
}

// Clock is "now" for lease and plan expiry. Tests jump it between rows.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Applier is the only code path that produces external effects. Ports
// are required; a missing port is fail-closed, not a skip.
type Applier struct {
	Kernel      *policy.Kernel
	Clock       Clock
	World       Executor
	Leases      Leaser
	Checkpoints Checkpointer
	Audit       Auditor
}

// Apply runs the fail-closed sequence. It never mutates the input Plan;
// the updated copy is Result.Plan. Partial failure wraps ErrPartial with
// AppliedThrough set to the number of rows that landed.
func (a *Applier) Apply(ctx context.Context, principal policy.PrincipalID, in Plan, approval Approval) (_ Result, err error) {
	if err := a.ready(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("plan apply: %w", err)
	}

	p := clonePlan(in)
	var (
		applied []Applied
		ck      CheckpointRef
	)

	switch p.Status {
	case StatusApplied:
		through := p.AppliedThrough
		if through == 0 {
			through = len(p.Rows)
		}
		return resultFrom(p, nil, through, p.Checkpoint), nil
	case StatusPartiallyApplied:
		return resultFrom(p, nil, p.AppliedThrough, p.Checkpoint), fmt.Errorf("plan apply: %w", ErrPartial)
	case StatusStale:
		return resultFrom(p, nil, 0, ""), fmt.Errorf("plan apply: %w", ErrStale)
	case StatusApproved, StatusApplying:
	default:
		return resultFrom(p, nil, 0, ""), fmt.Errorf("plan apply: %w", ErrNotApproved)
	}

	// Approval binds to HashOf, the canonical digest, not a stored field
	// the caller could have left pointing at an older revision.
	digest := HashOf(p)
	if p.Hash != digest {
		return resultFrom(p, nil, 0, ""), fmt.Errorf("plan apply: %w", ErrHashMismatch)
	}
	if approval.PlanHash != digest {
		p.ReviewComment = "approval hash does not cover this plan"
		return resultFrom(p, nil, 0, ""), fmt.Errorf("plan apply: %w", ErrApproval)
	}

	kernel := a.Kernel
	if kernel == nil {
		kernel = policy.New()
	}
	clock := a.Clock
	if clock == nil {
		clock = systemClock{}
	}
	now := clock.Now()

	rows, permErr := p.Permitted(now)
	if permErr != nil {
		return resultFrom(p, nil, 0, ""), permErr
	}

	for _, row := range rows {
		if _, ok := a.World.Seen(row.IdempotencyKey); ok {
			continue
		}
		got, obsErr := a.World.Observe(ctx, row.Target)
		if obsErr != nil {
			p.Status = StatusStale
			p.ReviewComment = obsErr.Error()
			return resultFrom(p, nil, 0, ""), fmt.Errorf("plan apply: %w: %w", ErrStale, obsErr)
		}
		if got != row.Precondition {
			p.Status = StatusStale
			p.ReviewComment = fmt.Sprintf("precondition drift on %s", row.Target)
			return resultFrom(p, nil, 0, ""), fmt.Errorf("plan apply: %w", ErrStale)
		}
	}

	ref, ckErr := a.Checkpoints.Checkpoint(ctx)
	if ckErr != nil {
		return resultFrom(p, nil, 0, ""), fmt.Errorf("plan apply checkpoint: %w", ckErr)
	}
	ck = ref
	p.Checkpoint = ck

	leases, acqErr := a.Leases.Acquire(ctx, p)
	if acqErr != nil {
		return resultFrom(p, nil, 0, ck), fmt.Errorf("plan apply acquire: %w", acqErr)
	}
	defer func() {
		relErr := a.Leases.Release(context.WithoutCancel(ctx), leases)
		if relErr == nil {
			return
		}
		if err == nil {
			err = fmt.Errorf("plan apply release: %w", relErr)
		}
	}()

	for _, row := range rows {
		if _, ok := a.World.Seen(row.IdempotencyKey); ok {
			continue
		}
		if findings := secretscan.Scan(row.Target, []byte(row.Payload)); len(findings) > 0 {
			p.ReviewComment = fmt.Sprintf("secret %s in %s", findings[0].Rule, findings[0].Path)
			return resultFrom(p, nil, 0, ck), fmt.Errorf("plan apply: %w", ErrSecret)
		}
		if authErr := authorizeRow(kernel, clock.Now(), principal, p, row, leases); authErr != nil {
			p.ReviewComment = authErr.Error()
			return resultFrom(p, nil, 0, ck), authErr
		}
	}

	applied = make([]Applied, 0, len(rows))
	for i, row := range rows {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return a.haltPartial(ctx, p, applied, ck, ctxErr)
		}
		if post, ok := a.World.Seen(row.IdempotencyKey); ok {
			applied = append(applied, Applied{Key: row.IdempotencyKey, Index: i, Postcondition: post})
			continue
		}
		if authErr := authorizeRow(kernel, clock.Now(), principal, p, row, leases); authErr != nil {
			return a.haltPartial(ctx, p, applied, ck, authErr)
		}
		post, execErr := a.World.Execute(ctx, row)
		if execErr != nil {
			return a.haltPartial(ctx, p, applied, ck, fmt.Errorf("row %d: %w", i+1, execErr))
		}
		if signErr := a.Audit.SignRow(ctx, row, post); signErr != nil {
			applied = append(applied, Applied{Key: row.IdempotencyKey, Index: i, Postcondition: post})
			return a.haltPartial(ctx, p, applied, ck, fmt.Errorf("sign row %d: %w", i+1, signErr))
		}
		applied = append(applied, Applied{Key: row.IdempotencyKey, Index: i, Postcondition: post})
	}

	p.Status = StatusApplied
	p.AppliedThrough = len(applied)
	p.Checkpoint = ck
	p.ReviewComment = ""
	if signErr := a.Audit.SignPlan(ctx, p); signErr != nil {
		return resultFrom(p, applied, p.AppliedThrough, ck), fmt.Errorf("plan apply: sign plan: %w", signErr)
	}
	return resultFrom(p, applied, p.AppliedThrough, ck), nil
}

func authorizeRow(k *policy.Kernel, now time.Time, principal policy.PrincipalID, p Plan, row Row, leases []policy.Lease) error {
	named, ok := leaseByID(leases, row.Lease)
	if !ok {
		return fmt.Errorf("plan apply: %w: no lease %s", ErrDenied, row.Lease)
	}
	d := k.AuthorizePlan(now, principal, p.PolicyPlan(), row.PolicyEffect(p.Scope), []policy.Lease{named})
	if !d.Allowed {
		return fmt.Errorf("plan apply: %w: %s", ErrDenied, d.Reason)
	}
	return nil
}

func leaseByID(leases []policy.Lease, id policy.LeaseID) (policy.Lease, bool) {
	for _, l := range leases {
		if l.ID == id {
			return l, true
		}
	}
	return policy.Lease{}, false
}

func (a *Applier) haltPartial(ctx context.Context, p Plan, applied []Applied, ck CheckpointRef, cause error) (Result, error) {
	if len(applied) == 0 {
		p.ReviewComment = cause.Error()
		return resultFrom(p, applied, 0, ck), fmt.Errorf("plan apply: %w", cause)
	}
	p.Status = StatusPartiallyApplied
	p.AppliedThrough = len(applied)
	p.Checkpoint = ck
	p.ReviewComment = cause.Error()
	_ = a.Audit.SignPlan(ctx, p)
	return resultFrom(p, applied, p.AppliedThrough, ck), fmt.Errorf("plan apply: %w: %w", ErrPartial, cause)
}

func (a *Applier) ready() error {
	if a == nil || a.World == nil || a.Leases == nil || a.Checkpoints == nil || a.Audit == nil {
		return fmt.Errorf("plan apply: %w", ErrInvalid)
	}
	return nil
}

func clonePlan(p Plan) Plan {
	out := p
	if p.Rows != nil {
		out.Rows = append([]Row(nil), p.Rows...)
	}
	if p.Credentials != nil {
		out.Credentials = append([]Credential(nil), p.Credentials...)
	}
	if p.Findings != nil {
		out.Findings = append([]Finding(nil), p.Findings...)
	}
	if p.CrossExam != nil {
		cx := *p.CrossExam
		out.CrossExam = &cx
	}
	return out
}
