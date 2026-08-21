package plan

import (
	"fmt"
	"strings"
	"time"
)

// Render returns a stable, operator-facing text form of the plan. Store
// round-trip must reproduce this byte-for-byte: the rendered plan is what
// was approved, not a pretty-printer's latest opinion.
func (p Plan) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "plan %s\n", p.Hash)
	fmt.Fprintf(&b, "status %s\n", p.Status)
	fmt.Fprintf(&b, "summary %s\n", p.Summary)
	fmt.Fprintf(&b, "expires %s\n", p.ExpiresAt.UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "cost_ceiling %d\n", p.CostCeiling)
	fmt.Fprintf(&b, "scope %s\n", p.Scope)
	b.WriteString("credentials\n")
	creds := canonicalCreds(p.Credentials)
	if len(creds) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, c := range creds {
		fmt.Fprintf(&b, "  %s %s\n", c.Provider, c.Kind)
	}
	b.WriteString("effects\n")
	for _, r := range p.Rows {
		fmt.Fprintf(&b, "%s %s\n", r.Op.Symbol(), r.Target)
		fmt.Fprintf(&b, "  lease %s\n", r.Lease)
		fmt.Fprintf(&b, "  pre %s\n", r.Precondition)
		fmt.Fprintf(&b, "  post %s\n", r.Postcondition)
		fmt.Fprintf(&b, "  idem %s\n", r.IdempotencyKey)
		if r.CostEstimate != "" {
			fmt.Fprintf(&b, "  cost %s\n", r.CostEstimate)
		}
		b.WriteString("  payload\n")
		for _, line := range strings.Split(r.Payload, "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	return b.String()
}
