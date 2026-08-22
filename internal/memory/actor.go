package memory

import "strings"

// ActorKind is who performed a notebook action. Human and agent are
// distinct values, not a preference flag.
type ActorKind string

const (
	// ActorHuman writes, accepts, rejects, and deletes.
	ActorHuman ActorKind = "human"
	// ActorAgent only proposes. There is no agent-only notebook path.
	ActorAgent ActorKind = "agent"
)

// Actor is who/what performed an action. Name is a label (operator,
// agent id), not an interchangeable id type.
type Actor struct {
	Kind ActorKind
	Name string
}

// Human returns a human actor. Empty name is invalid at the call site.
func Human(name string) Actor {
	return Actor{Kind: ActorHuman, Name: strings.TrimSpace(name)}
}

// AgentActor returns an agent actor. Named AgentActor so it does not
// clash with store agent records.
func AgentActor(name string) Actor {
	return Actor{Kind: ActorAgent, Name: strings.TrimSpace(name)}
}

// IsHuman reports whether a is a human actor with a name.
func (a Actor) IsHuman() bool {
	return a.Kind == ActorHuman && a.Name != ""
}

// IsAgent reports whether a is an agent actor with a name.
func (a Actor) IsAgent() bool {
	return a.Kind == ActorAgent && a.Name != ""
}

func (a Actor) valid() bool {
	return a.IsHuman() || a.IsAgent()
}
