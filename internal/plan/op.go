package plan

import "github.com/avivl/zeroth/internal/policy"

// Op is one resource-row operation. The four values are the closed set
// the builder can express. Anything else is unexpressible.
type Op string

const (
	// OpCreate is "+": the target does not exist at draft time.
	OpCreate Op = "create"
	// OpModify is "~": the target exists and its bytes will change.
	OpModify Op = "modify"
	// OpDestroy is "−": the target exists and will be removed.
	OpDestroy Op = "destroy"
	// OpMemoryProposal is "○": a memory row for human review, not a file.
	OpMemoryProposal Op = "memory_proposal"
)

// Symbol is the operator-facing glyph for this op.
func (op Op) Symbol() string {
	switch op {
	case OpCreate:
		return "+"
	case OpModify:
		return "~"
	case OpDestroy:
		return "\u2212"
	case OpMemoryProposal:
		return "\u25cb"
	default:
		return ""
	}
}

// Kind is the policy effect kind apply will ask the kernel to authorize.
// The strings match the OpenAPI PlanEffect types so the plan, the harness,
// and the wire format share one vocabulary.
func (op Op) Kind() policy.EffectKind {
	return policy.EffectKind(op)
}

// ParseOp reports whether s is one of the four expressible operations.
func ParseOp(s string) (Op, bool) {
	switch Op(s) {
	case OpCreate, OpModify, OpDestroy, OpMemoryProposal:
		return Op(s), true
	default:
		return "", false
	}
}
