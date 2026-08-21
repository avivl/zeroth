package plan

import (
	"fmt"
	"time"
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
