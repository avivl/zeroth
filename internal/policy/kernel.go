package policy

import "time"

// Kernel answers "may this principal, in this scope, holding these leases,
// perform this effect?" It holds no state: every call receives the full set
// of leases and plans it is allowed to consider, and returns a Decision.
// This is deliberate. A kernel with no I/O and no internal store cannot
// drift from the audit log, because it has nowhere else to keep an opinion.
type Kernel struct{}

// New returns a ready-to-use Kernel. It exists mainly so call sites read
// policy.New() rather than constructing the zero value directly; either
// works, the zero value has no invalid state.
func New() *Kernel {
	return &Kernel{}
}

// Authorize checks a single effect against the leases held by principal.
// Deny by default: the effect is allowed only if at least one lease, scoped
// to the same principal and scope, still valid at now, covers its kind.
// Every other lease in the slice, including expired ones and ones scoped to
// a different principal or scope, is inert for this call.
func (k *Kernel) Authorize(now time.Time, principal PrincipalID, effect Effect, leases []Lease) Decision {
	for _, l := range leases {
		if l.Principal != principal {
			continue
		}
		if l.Scope != effect.Scope {
			continue
		}
		if !l.Valid(now) {
			continue
		}
		if l.Covers(effect.Kind) {
			return allow("lease %s covers %s in scope %s", l.ID, effect.Kind, effect.Scope)
		}
	}
	return deny("no valid lease for principal %s covers %s in scope %s", principal, effect.Kind, effect.Scope)
}

// AuthorizePlan checks an effect against an approved plan and the leases
// held by principal. The effect must appear in approved.Effects and be
// covered by a valid lease; either failing is a deny. This is what makes
// plan approval scope-tight: approving a plan never authorizes an effect
// the plan itself does not list, regardless of what leases the principal
// separately holds.
func (k *Kernel) AuthorizePlan(now time.Time, principal PrincipalID, approved Plan, effect Effect, leases []Lease) Decision {
	if !approved.Authorizes(effect) {
		return deny("effect %s on %s not present in approved plan %s", effect.Kind, effect.Target, approved.Hash)
	}
	return k.Authorize(now, principal, effect, leases)
}
