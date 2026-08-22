package plan

import (
	"errors"
	"fmt"
)

var (
	// ErrUnexpressible is wrapped when a proposed effect cannot be a row.
	// The run must stop and ask. There is no direct-action fallback.
	ErrUnexpressible = errors.New("unexpressible effect")
	// ErrNotApproved is returned when apply is asked of a plan that the
	// operator has not approved (or that is no longer in an apply-able
	// status).
	ErrNotApproved = errors.New("plan not approved")
	// ErrExpired is returned when the plan's expiry has been reached.
	// Expiry is exclusive, matching policy leases.
	ErrExpired = errors.New("plan expired")
	// ErrNotInPlan is returned when apply is asked to perform a row the
	// approved plan does not list.
	ErrNotInPlan = errors.New("effect not in approved plan")
	// ErrHashMismatch is returned when the stored hash does not match the
	// canonical hash of the rows and constraints. A mismatch is a
	// revision, not an approved plan.
	ErrHashMismatch = errors.New("plan hash mismatch")
	// ErrInvalid is returned for empty identifiers, missing expiry, or
	// other draft inputs that are not an effect-level failure.
	ErrInvalid = errors.New("invalid plan")
	// ErrStale is returned when a precondition no longer matches. Nothing
	// was written. The plan is marked stale so the agent re-drafts.
	ErrStale = errors.New("plan: stale preconditions")
	// ErrPostcondition is returned when the bytes that landed do not
	// hash to the row's recorded postcondition. Nothing is published.
	ErrPostcondition = errors.New("plan: postcondition mismatch")
	// ErrApproval is returned when the approval's plan hash is not this
	// plan's hash. Approving rev2 does not authorize rev3.
	ErrApproval = errors.New("plan: approval does not cover this plan hash")
	// ErrPartial is returned when some prefix of the rows was applied and
	// the rest was not. Applied rows stay applied. The boundary is on
	// Result.AppliedThrough.
	ErrPartial = errors.New("plan: partially applied")
	// ErrDenied is returned when the policy kernel denies a row before
	// any new write. Mid-apply denials wrap ErrPartial instead.
	ErrDenied = errors.New("plan: kernel denied")
	// ErrSecret is returned when secretscan finds a leak in a row that
	// has not yet been applied. Nothing is written.
	ErrSecret = errors.New("plan: secret scan blocked apply")
	// ErrNoReviewer is returned when cross-exam is asked with no
	// reviewer model configured. Missing review is a deny, not a skip.
	ErrNoReviewer = errors.New("plan: no reviewer configured")
	// ErrSameModel is returned when a reviewer model equals the
	// producer or another reviewer. Same-model second pass is not
	// diversity and does not count (Z1-019).
	ErrSameModel = errors.New("plan: reviewer must differ from producer")
	// ErrNotExamined is returned when approve is asked of a plan that
	// has not been cross-examined. The human gate is after the
	// reviewer, not instead of it (Z1-019).
	ErrNotExamined = errors.New("plan: not cross-examined")
)

// UnexpressibleError names the proposed effect that could not become a
// row and why. Unwraps to [ErrUnexpressible].
type UnexpressibleError struct {
	Index  int
	Type   string
	Target string
	Reason string
}

func (e *UnexpressibleError) Error() string {
	if e == nil {
		return "plan builder: unexpressible effect"
	}
	return fmt.Sprintf("plan builder: effect %d (%s %q) cannot be expressed as a plan row: %s",
		e.Index, e.Type, e.Target, e.Reason)
}

func (e *UnexpressibleError) Unwrap() error { return ErrUnexpressible }

// StatusError is a lifecycle operation attempted in the wrong status.
type StatusError struct {
	Op     string
	Status Status
}

func (e *StatusError) Error() string {
	if e == nil {
		return "plan: illegal status"
	}
	st := e.Status
	if st == "" {
		st = "<empty>"
	}
	return fmt.Sprintf("%s: status %s", e.Op, st)
}
