package memory

import "strings"

// ForSession returns the live facts a run may compile at hydration:
// operator-global rows, this session, and this agent. Other sessions
// stay out of the sandbox overlay.
func ForSession(facts []Fact, sessionID, agentID string) []Fact {
	out := make([]Fact, 0, len(facts))
	for _, f := range facts {
		if f.Deleted {
			continue
		}
		switch f.Kind {
		case KindOperator:
			out = append(out, f)
		case KindSession:
			if sessionID != "" && f.RefID == sessionID {
				out = append(out, f)
			}
		case KindAgent:
			if agentID != "" && f.RefID == agentID {
				out = append(out, f)
			}
		}
	}
	sortFacts(out)
	return out
}

// ParseProposalTarget splits a plan memory_proposal target into notebook
// kind, ref, and key. "operator/style" is global. "session/style" and
// "agent/style" take sessionID and agentID as ref. A bare key is
// session-scoped for this run.
func ParseProposalTarget(target, sessionID, agentID string) (kind, refID, key string) {
	target = strings.TrimSpace(strings.ReplaceAll(target, "\\", "/"))
	target = strings.TrimPrefix(target, "./")
	if target == "" {
		return KindSession, sessionID, ""
	}
	head, rest, ok := strings.Cut(target, "/")
	if !ok || strings.TrimSpace(rest) == "" {
		return KindSession, sessionID, target
	}
	switch head {
	case KindOperator:
		return KindOperator, "", rest
	case KindSession:
		return KindSession, sessionID, rest
	case KindAgent:
		return KindAgent, agentID, rest
	default:
		return KindSession, sessionID, target
	}
}
