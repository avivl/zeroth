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
