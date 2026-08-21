package plan

import (
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/policy"
)

func TestHashIndependentRowOrder(t *testing.T) {
	t.Parallel()
	a := Row{Op: OpModify, Target: "a.md", Payload: "A", Lease: "L1", Precondition: "pa", IdempotencyKey: "ia", Postcondition: "qa"}
	b := Row{Op: OpModify, Target: "b.md", Payload: "B", Lease: "L1", Precondition: "pb", IdempotencyKey: "ib", Postcondition: "qb"}
	left := samplePlan(t, []Row{a, b})
	right := samplePlan(t, []Row{b, a})
	if HashOf(left) != HashOf(right) {
		t.Fatalf("independent rows must hash the same: %s vs %s", HashOf(left), HashOf(right))
	}
	if left.Render() == right.Render() {
		t.Fatal("render keeps proposed order, so it should differ")
	}
}

func TestHashSameTargetOrderMatters(t *testing.T) {
	t.Parallel()
	create := Row{Op: OpCreate, Target: "a.md", Payload: "new", Lease: "L1", IdempotencyKey: "ic", Postcondition: "qc"}
	modify := Row{Op: OpModify, Target: "a.md", Payload: "chg", Lease: "L1", Precondition: "p", IdempotencyKey: "im", Postcondition: "qm"}
	left := samplePlan(t, []Row{create, modify})
	right := samplePlan(t, []Row{modify, create})
	if HashOf(left) == HashOf(right) {
		t.Fatal("same-target order must change the hash")
	}
}

func TestHashStableAcrossCopy(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	q := p
	q.Rows = append([]Row(nil), p.Rows...)
	if HashOf(p) != HashOf(q) || p.Hash != HashOf(q) {
		t.Fatalf("copy changed hash: %s vs %s", HashOf(p), HashOf(q))
	}
}

func TestHashIgnoresStatusAndReview(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	q := p
	q.Status = StatusApproved
	q.ReviewComment = "ship it"
	q.UpdatedAt = p.UpdatedAt.Add(time.Hour)
	if HashOf(p) != HashOf(q) {
		t.Fatal("approve must not change the canonical hash")
	}
}

func TestHashCredentialOrder(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{Credentials: []Credential{
		{Provider: "github", Kind: "pat"},
		{Provider: "anthropic", Kind: "api_key"},
	}})
	q := mustBuild(t, Draft{Credentials: []Credential{
		{Provider: "anthropic", Kind: "api_key"},
		{Provider: "github", Kind: "pat"},
	}})
	if HashOf(p) != HashOf(q) {
		t.Fatal("credential order must not change the hash")
	}
}

func TestHashRevision(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	q := p
	q.Rows = append([]Row(nil), p.Rows...)
	q.Rows[0].Payload = "other"
	if HashOf(p) == HashOf(q) {
		t.Fatal("payload change must be a new hash")
	}
	if policy.PlanHash("") == HashOf(p) {
		t.Fatal("hash must not be empty")
	}
}

func samplePlan(t *testing.T, rows []Row) Plan {
	t.Helper()
	p := mustBuild(t, Draft{})
	p.Rows = rows
	p.Hash = HashOf(p)
	return p
}
