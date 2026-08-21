package plan

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/policy"
)

const propertyIterations = 500

func TestPropertyApprovedPlanIsExactApplySet(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)

	for i := 0; i < propertyIterations; i++ {
		n := r.Intn(4) + 1
		rows := make([]Row, n)
		for j := 0; j < n; j++ {
			rows[j] = randRow(r, j)
		}
		p := Plan{
			Status:      StatusApproved,
			Summary:     fmt.Sprintf("plan-%d", i),
			ExpiresAt:   expires,
			CostCeiling: int64(r.Intn(1000)),
			Scope:       policy.ScopeID("scope-a"),
			Rows:        rows,
		}
		p.Hash = HashOf(p)

		permitted, err := p.Permitted(now)
		if err != nil {
			t.Fatalf("iteration %d: Permitted: %v", i, err)
		}
		if len(permitted) != len(p.Rows) {
			t.Fatalf("iteration %d: permitted %d want %d", i, len(permitted), len(p.Rows))
		}
		for _, row := range p.Rows {
			if err := p.CanApply(row, now); err != nil {
				t.Fatalf("iteration %d: listed row denied: %v", i, err)
			}
		}

		probe := randRow(r, n+10)
		listed := p.contains(probe)
		err = p.CanApply(probe, now)
		if listed {
			if err != nil {
				t.Fatalf("iteration %d: listed probe denied: %v probe=%+v", i, err, probe)
			}
		} else if !errors.Is(err, ErrNotInPlan) {
			t.Fatalf("iteration %d: unlisted probe err=%v want NotInPlan", i, err)
		}

		k := policy.New()
		effect := probe.PolicyEffect(p.Scope)
		cover := policy.NewLease("omni", p.Scope, "alice", expires, effect.Kind)
		got := k.AuthorizePlan(now, "alice", p.PolicyPlan(), effect, []policy.Lease{cover})
		// The kernel matches kind/scope/target. Apply matches the full row.
		// A probe that shares a listed target can pass the kernel and still
		// fail CanApply; the property is that apply never exceeds the row set.
		if err == nil && !got.Allowed && listed {
			t.Fatalf("iteration %d: apply allowed a listed row the kernel denied", i)
		}
		if err == nil && !listed {
			t.Fatalf("iteration %d: apply allowed a row not in the plan", i)
		}
	}
}

func TestCanApplyDeniesDraftExpiredAndTampered(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	p := mustBuild(t, Draft{})
	row := p.Rows[0]
	if err := p.CanApply(row, now); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("draft apply: %v", err)
	}
	approved, err := p.Approve(now)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Hash != p.Hash {
		t.Fatal("approve changed hash")
	}
	if err := approved.CanApply(row, now); err != nil {
		t.Fatalf("approved: %v", err)
	}
	if err := approved.CanApply(row, approved.ExpiresAt); !errors.Is(err, ErrExpired) {
		t.Fatalf("at expiry: %v", err)
	}
	tampered := approved
	tampered.Rows = append([]Row(nil), approved.Rows...)
	tampered.Rows[0].Payload = "nope"
	if err := tampered.CanApply(tampered.Rows[0], now); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("tampered: %v", err)
	}
}

func randRow(r *rand.Rand, i int) Row {
	ops := []Op{OpCreate, OpModify, OpDestroy, OpMemoryProposal}
	op := ops[r.Intn(len(ops))]
	target := fmt.Sprintf("f-%d.md", r.Intn(6))
	payload := fmt.Sprintf("body-%d-%d", i, r.Intn(8))
	pre := ""
	if op == OpModify || op == OpDestroy {
		pre = fmt.Sprintf("pre-%d", r.Intn(8))
	}
	return Row{
		Op:             op,
		Target:         target,
		Payload:        payload,
		Lease:          policy.LeaseID(fmt.Sprintf("lease-%d", r.Intn(3))),
		Precondition:   pre,
		IdempotencyKey: fmt.Sprintf("idem-%d", i),
		Postcondition:  fmt.Sprintf("post-%d", r.Intn(8)),
	}
}
