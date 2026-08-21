package policy

import "time"

// Lease grants a principal the right to perform a bounded set of effect
// kinds within one scope, until it expires. A lease that has expired is
// inert: the kernel treats it exactly as if it did not exist, it is never
// specially reported or half-honored.
type Lease struct {
	ID        LeaseID
	Scope     ScopeID
	Principal PrincipalID
	Kinds     map[EffectKind]struct{}
	ExpiresAt time.Time
}

// Covers reports whether this lease's grant includes the given effect kind.
func (l Lease) Covers(kind EffectKind) bool {
	if l.Kinds == nil {
		return false
	}
	_, ok := l.Kinds[kind]
	return ok
}

// Valid reports whether the lease has not yet expired at now. Expiry is
// exclusive: a lease is valid up to, and not including, its ExpiresAt
// instant.
func (l Lease) Valid(now time.Time) bool {
	return now.Before(l.ExpiresAt)
}

// NewLease builds a lease covering the given effect kinds. It is a
// constructor, not a grant: nothing is authorized until the kernel is asked
// to authorize an effect against a set of leases that includes this one.
func NewLease(id LeaseID, scope ScopeID, principal PrincipalID, expiresAt time.Time, kinds ...EffectKind) Lease {
	set := make(map[EffectKind]struct{}, len(kinds))
	for _, k := range kinds {
		set[k] = struct{}{}
	}
	return Lease{
		ID:        id,
		Scope:     scope,
		Principal: principal,
		Kinds:     set,
		ExpiresAt: expiresAt,
	}
}
