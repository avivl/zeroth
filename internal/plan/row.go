package plan

import "github.com/avivl/zeroth/internal/policy"

// Row is one resource mutation the operator reviews and apply may
// execute. The fields are the ones the issue names: operation, target,
// payload, lease, precondition, idempotency key, expected postcondition.
type Row struct {
	Op             Op
	Target         string
	Payload        string
	Lease          policy.LeaseID
	Precondition   string
	IdempotencyKey string
	Postcondition  string
	// CostEstimate is an optional harness hint. It is not the plan-level
	// ceiling and it is not a billing field.
	CostEstimate string
}

// PolicyEffect projects this row onto the kernel's effect type. The
// kernel matches on kind, scope, and target; payload and hashes stay in
// this package, which is what makes apply exact rather than "same path."
func (r Row) PolicyEffect(scope policy.ScopeID) policy.Effect {
	return policy.Effect{
		Kind:   r.Op.Kind(),
		Scope:  scope,
		Target: r.Target,
	}
}

func (r Row) equal(o Row) bool {
	return r.Op == o.Op &&
		r.Target == o.Target &&
		r.Payload == o.Payload &&
		r.Lease == o.Lease &&
		r.Precondition == o.Precondition &&
		r.IdempotencyKey == o.IdempotencyKey &&
		r.Postcondition == o.Postcondition &&
		r.CostEstimate == o.CostEstimate
}
