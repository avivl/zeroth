package policy

// Property tests for the three invariants the kernel exists to guarantee.
// These are written against the standard library's math/rand rather than
// pgregory.net/rapid or gopter: this package was drafted in a sandbox with
// no network egress to the Go module proxy, so an external property-testing
// dependency could not be fetched or verified here. The three properties
// below are the ones the ticket calls out by name, each run over many
// randomized cases with a fixed seed for reproducibility. Swapping the
// generators onto rapid or gopter later is a mechanical change, the
// invariants and the assertions do not need to move; rapid in particular
// would add input shrinking on failure, which this version does not have.

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

const propertyIterations = 2000

func randScope(r *rand.Rand, n int) ScopeID {
	return ScopeID(fmt.Sprintf("scope-%d", r.Intn(n)))
}

func randPrincipal(r *rand.Rand, n int) PrincipalID {
	return PrincipalID(fmt.Sprintf("principal-%d", r.Intn(n)))
}

func randKind(r *rand.Rand, n int) EffectKind {
	return EffectKind(fmt.Sprintf("kind-%d", r.Intn(n)))
}

func randEffect(r *rand.Rand, scopes, kinds int) Effect {
	return Effect{
		Kind:   randKind(r, kinds),
		Scope:  randScope(r, scopes),
		Target: fmt.Sprintf("target-%d", r.Intn(5)),
	}
}

// randLease builds a lease whose expiry is randomly before or after refTime,
// so generated fixtures exercise both valid and expired leases.
func randLease(r *rand.Rand, id int, scopes, principals, kinds int, refTime time.Time) Lease {
	offset := time.Duration(r.Intn(7200)-3600) * time.Second // -1h .. +1h around refTime
	numKinds := r.Intn(kinds + 1)
	set := make([]EffectKind, 0, numKinds)
	seen := make(map[EffectKind]struct{}, numKinds)
	for len(set) < numKinds {
		k := randKind(r, kinds)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		set = append(set, k)
	}
	return NewLease(
		LeaseID(fmt.Sprintf("lease-%d", id)),
		randScope(r, scopes),
		randPrincipal(r, principals),
		refTime.Add(offset),
		set...,
	)
}

// TestPropertyNoAccessToUngrantedScope: no grant sequence yields access to a
// scope that never granted it. For any randomly generated set of leases and
// any probe effect in a scope none of those leases mention, Authorize must
// deny, regardless of what the leases grant elsewhere.
func TestPropertyNoAccessToUngrantedScope(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	refTime := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	for i := 0; i < propertyIterations; i++ {
		principal := randPrincipal(r, 3)
		numLeases := r.Intn(6)
		leases := make([]Lease, 0, numLeases)
		grantedScopes := make(map[ScopeID]struct{})
		for j := 0; j < numLeases; j++ {
			l := randLease(r, j, 4, 3, 3, refTime)
			leases = append(leases, l)
			grantedScopes[l.Scope] = struct{}{}
		}

		// Build a probe effect in a scope index outside the range leases
		// were generated from, so it is guaranteed ungranted by construction
		// (leases above are scoped 0..3, this probe uses scope index 99).
		probe := Effect{Kind: randKind(r, 3), Scope: ScopeID("scope-99"), Target: "t"}
		if _, ok := grantedScopes[probe.Scope]; ok {
			t.Fatalf("test construction bug: probe scope %q was granted", probe.Scope)
		}

		k := New()
		got := k.Authorize(refTime, principal, probe, leases)
		if got.Allowed {
			t.Fatalf("iteration %d: Authorize allowed access to an ungranted scope: leases=%+v probe=%+v decision=%+v",
				i, leases, probe, got)
		}
	}
}

// TestPropertyNoLeaseUsableAfterExpiry: no lease is usable after its
// expiry. For any randomly generated lease that fully covers a probe
// effect, Authorize allows exactly when the lease is still valid at the
// query time, and denies once the query time has passed the lease's
// expiry, with no exception.
func TestPropertyNoLeaseUsableAfterExpiry(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	refTime := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	for i := 0; i < propertyIterations; i++ {
		principal := randPrincipal(r, 1) // single principal, no scope/principal noise
		scope := randScope(r, 1)
		kind := randKind(r, 1)
		effect := Effect{Kind: kind, Scope: scope, Target: "t"}

		expiresAt := refTime.Add(time.Duration(r.Intn(7200)-3600) * time.Second)
		l := NewLease("only-lease", scope, principal, expiresAt, kind)

		k := New()
		got := k.Authorize(refTime, principal, effect, []Lease{l})

		wantAllow := refTime.Before(expiresAt)
		if got.Allowed != wantAllow {
			t.Fatalf("iteration %d: lease expiring at %s, queried at %s: Authorize=%v want=%v",
				i, expiresAt, refTime, got.Allowed, wantAllow)
		}
	}
}

// TestPropertyPlanApprovalIsExact: approving plan A never authorizes any
// effect outside A's effect set. For any randomly generated plan and any
// probe effect, AuthorizePlan allows only if the probe is literally one of
// the plan's listed effects (and a lease covers it); an effect that
// resembles a listed one but differs in kind, scope, or target must never
// be authorized just because it is "close."
func TestPropertyPlanApprovalIsExact(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	refTime := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	principal := PrincipalID("alice")

	for i := 0; i < propertyIterations; i++ {
		numPlanEffects := r.Intn(5) + 1
		planEffects := make([]Effect, 0, numPlanEffects)
		for j := 0; j < numPlanEffects; j++ {
			planEffects = append(planEffects, randEffect(r, 3, 3))
		}
		plan := Plan{Hash: PlanHash(fmt.Sprintf("plan-%d", i)), Effects: planEffects}

		probe := randEffect(r, 3, 3)

		// Always-valid, all-covering lease: if the kernel denies here, it
		// can only be because the plan itself did not authorize the probe,
		// isolating exactly the property under test from lease coverage.
		coversEverything := NewLease("omni", probe.Scope, principal, refTime.Add(time.Hour), probe.Kind)

		k := New()
		got := k.AuthorizePlan(refTime, principal, plan, probe, []Lease{coversEverything})

		wantAllow := plan.Authorizes(probe)
		if got.Allowed != wantAllow {
			t.Fatalf("iteration %d: plan=%+v probe=%+v: AuthorizePlan=%v want=%v (decision=%+v)",
				i, plan, probe, got.Allowed, wantAllow, got)
		}
	}
}
