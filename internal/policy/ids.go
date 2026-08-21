package policy

// Distinct named ID types. Each is its own Go type, not an alias, so the
// compiler rejects passing a LeaseID where a ScopeID is expected. That is
// not pedantry: interchangeable IDs are exactly the class of bug that turns
// into a cross-scope leak, silently, at a call site nobody is looking at.
type (
	ScopeID     string
	PrincipalID string
	AgentID     string
	LeaseID     string
	PlanHash    string
)
