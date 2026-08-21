package policy

// Plan is an approved bundle of effects, identified by its canonical hash.
// Approving a plan authorizes exactly the effects listed on it: nothing
// outside that set, no matter what else a caller passes alongside it. The
// kernel enforces this by only ever consulting Plan.Effects, never the
// caller's own claim about what a plan covers.
type Plan struct {
	Hash    PlanHash
	Effects []Effect
}

// Authorizes reports whether e is one of the effects this plan lists. It is
// an exact match on kind, scope, and target: a plan that approves writing
// file A does not implicitly approve writing file B, even in the same scope
// and even for the same effect kind.
func (p Plan) Authorizes(e Effect) bool {
	for _, pe := range p.Effects {
		if pe == e {
			return true
		}
	}
	return false
}
