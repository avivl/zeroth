package policy

import (
	"testing"
	"time"
)

var (
	scopeA = ScopeID("scope-a")
	scopeB = ScopeID("scope-b")

	alice = PrincipalID("alice")
	bob   = PrincipalID("bob")

	writeFile EffectKind = "file_write"
	createPR  EffectKind = "pr_create"

	now = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
)

func effect(kind EffectKind, scope ScopeID, target string) Effect {
	return Effect{Kind: kind, Scope: scope, Target: target}
}

func lease(id LeaseID, scope ScopeID, principal PrincipalID, ttl time.Duration, kinds ...EffectKind) Lease {
	return NewLease(id, scope, principal, now.Add(ttl), kinds...)
}

func TestAuthorize(t *testing.T) {
	tests := []struct {
		name      string
		principal PrincipalID
		effect    Effect
		leases    []Lease
		wantAllow bool
	}{
		{
			name:      "no leases at all denies",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases:    nil,
			wantAllow: false,
		},
		{
			name:      "matching lease covering the kind allows",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases:    []Lease{lease("L1", scopeA, alice, time.Hour, writeFile)},
			wantAllow: true,
		},
		{
			name:      "lease for a different principal denies",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases:    []Lease{lease("L1", scopeA, bob, time.Hour, writeFile)},
			wantAllow: false,
		},
		{
			name:      "lease for a different scope denies",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases:    []Lease{lease("L1", scopeB, alice, time.Hour, writeFile)},
			wantAllow: false,
		},
		{
			name:      "lease that does not cover the effect kind denies",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases:    []Lease{lease("L1", scopeA, alice, time.Hour, createPR)},
			wantAllow: false,
		},
		{
			name:      "expired lease denies even though it would otherwise cover",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases:    []Lease{lease("L1", scopeA, alice, -time.Hour, writeFile)},
			wantAllow: false,
		},
		{
			name:      "lease expiring exactly at now denies, expiry is exclusive",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases:    []Lease{lease("L1", scopeA, alice, 0, writeFile)},
			wantAllow: false,
		},
		{
			name:      "one covering lease among several non-covering leases still allows",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases: []Lease{
				lease("L1", scopeA, bob, time.Hour, writeFile),
				lease("L2", scopeB, alice, time.Hour, writeFile),
				lease("L3", scopeA, alice, -time.Hour, writeFile),
				lease("L4", scopeA, alice, time.Hour, writeFile),
			},
			wantAllow: true,
		},
		{
			name:      "empty kinds set on a lease covers nothing",
			principal: alice,
			effect:    effect(writeFile, scopeA, "README.md"),
			leases:    []Lease{NewLease("L1", scopeA, alice, now.Add(time.Hour))},
			wantAllow: false,
		},
	}

	k := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := k.Authorize(now, tt.principal, tt.effect, tt.leases)
			if got.Allowed != tt.wantAllow {
				t.Fatalf("Authorize() = %+v, want Allowed=%v", got, tt.wantAllow)
			}
			if got.Reason == "" {
				t.Fatal("Decision.Reason must never be empty")
			}
		})
	}
}

func TestAuthorizePlan(t *testing.T) {
	inPlan := effect(writeFile, scopeA, "README.md")
	outOfPlan := effect(writeFile, scopeA, "other.md")
	planHash := PlanHash("plan-123")

	tests := []struct {
		name      string
		effect    Effect
		leases    []Lease
		wantAllow bool
	}{
		{
			name:      "effect in plan and covered by a lease allows",
			effect:    inPlan,
			leases:    []Lease{lease("L1", scopeA, alice, time.Hour, writeFile)},
			wantAllow: true,
		},
		{
			name:      "effect not in plan denies even with a covering lease",
			effect:    outOfPlan,
			leases:    []Lease{lease("L1", scopeA, alice, time.Hour, writeFile)},
			wantAllow: false,
		},
		{
			name:      "effect in plan but no covering lease denies",
			effect:    inPlan,
			leases:    nil,
			wantAllow: false,
		},
		{
			name:      "effect in plan but only an expired lease denies",
			effect:    inPlan,
			leases:    []Lease{lease("L1", scopeA, alice, -time.Hour, writeFile)},
			wantAllow: false,
		},
	}

	plan := Plan{Hash: planHash, Effects: []Effect{inPlan}}
	k := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := k.AuthorizePlan(now, alice, plan, tt.effect, tt.leases)
			if got.Allowed != tt.wantAllow {
				t.Fatalf("AuthorizePlan() = %+v, want Allowed=%v", got, tt.wantAllow)
			}
			if got.Reason == "" {
				t.Fatal("Decision.Reason must never be empty")
			}
		})
	}
}

func TestPlanAuthorizesIsExactMatch(t *testing.T) {
	base := effect(writeFile, scopeA, "README.md")
	plan := Plan{Hash: "p1", Effects: []Effect{base}}

	if !plan.Authorizes(base) {
		t.Fatal("plan should authorize the exact effect it lists")
	}

	variants := []Effect{
		{Kind: createPR, Scope: scopeA, Target: "README.md"},  // different kind
		{Kind: writeFile, Scope: scopeB, Target: "README.md"}, // different scope
		{Kind: writeFile, Scope: scopeA, Target: "other.md"},  // different target
	}
	for _, v := range variants {
		if plan.Authorizes(v) {
			t.Fatalf("plan must not authorize %+v, it only lists %+v", v, base)
		}
	}
}

func TestLeaseCoversNilKindsIsFalse(t *testing.T) {
	var l Lease
	if l.Covers(writeFile) {
		t.Fatal("a lease with a nil Kinds map must not cover anything")
	}
}

func TestLeaseValidBoundary(t *testing.T) {
	l := NewLease("L1", scopeA, alice, now, writeFile)
	if l.Valid(now) {
		t.Fatal("a lease must not be valid at the exact instant it expires")
	}
	if !l.Valid(now.Add(-time.Nanosecond)) {
		t.Fatal("a lease must be valid one nanosecond before it expires")
	}
}
