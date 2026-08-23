package harness

import "context"

// Driver drives an agent runtime through a session.
//
// A Driver is a port. Concrete harnesses implement this interface;
// zerothd depends on the port, not the vendor. Guarantees below are the
// conformance contract. A second adapter must pass the same suite
// unchanged except for adding its table row (Z1-006, NFR-4).
type Driver interface {
	// Name is a stable identifier used in logs, audit records, and
	// conformance tests (for example "claudecode").
	Name() string

	// Start launches a supervised agent run against spec.Workspace.
	// The returned handle is live until Stop. Start does not apply
	// effects: the agent proposes them. Empty Prompt or Workspace is
	// ErrInvalid. Env is KEY=value for this process only; the API key
	// (ADR-Z-0008) belongs here, never in the workspace.
	Start(ctx context.Context, spec Spec) (Handle, error)

	// Stream yields events from the run: tokens, tool calls, proposed
	// effects, errors, and process exit. Historical events are replayed
	// first, then live ones. The channel closes after Stop (or when ctx
	// is cancelled for that subscriber). An unexpected subprocess death
	// is an EventExited, not a hang.
	Stream(ctx context.Context, id ID) (<-chan Event, error)

	// Steer injects operator guidance into a live run. Empty msg is
	// ErrInvalid. If the subprocess has already exited, the driver
	// restarts it with the guidance rather than dropping the message.
	Steer(ctx context.Context, id ID, msg string) error

	// Checkpoint captures enough vendor session state to resume later
	// via Spec.Resume. It does not stop the run.
	Checkpoint(ctx context.Context, id ID) (Checkpoint, error)

	// Stop kills the subprocess and releases the id. It is idempotent:
	// Stop on an unknown or already-stopped id returns nil. Further
	// Steer after Stop returns ErrStopped (or ErrNotFound).
	Stop(ctx context.Context, id ID) error
}

// Spec is what Start needs to run one agent turn.
type Spec struct {
	// Workspace is the directory the subprocess uses as cwd. It is the
	// sandbox overlay's host path (HostWorkspace). Stage 1 runs the
	// agent as a host subprocess against that tree, not inside
	// sandbox.Exec (ADR-Z-0010). The driver must not write credentials
	// into it.
	Workspace string
	// Prompt is the user task. Empty is ErrInvalid.
	Prompt string
	// Env is KEY=value pairs for the subprocess. Malformed entries (no
	// '=') are ErrInvalid. ANTHROPIC_API_KEY belongs here when the
	// caller holds it. Values are never written into Workspace.
	Env []string
	// Resume, when VendorSession is set, continues a prior Checkpoint.
	Resume Checkpoint
}

// Handle is what Start returns. Subsequent calls take ID.
type Handle struct {
	ID ID
}

// Checkpoint is enough state to resume a run. It is not a sandbox tar.
type Checkpoint struct {
	VendorSession string
	Transcript    string
}

// EventKind names a Stream row.
type EventKind string

const (
	// EventToken is one agent output chunk.
	EventToken EventKind = "token"
	// EventToolCall is one tool invocation the agent requested.
	EventToolCall EventKind = "tool_call"
	// EventEffects is a structured proposed-effect set for the plan builder.
	EventEffects EventKind = "effects"
	// EventError is a non-terminal driver or vendor error.
	EventError EventKind = "error"
	// EventExited records subprocess exit (clean, signal, or Stop). A
	// driver must not report a truncated or errored output stream as a
	// clean exit: the payload has to distinguish the two.
	EventExited EventKind = "exited"
)

// Event is one Stream record. Payload is redacted of credentials.
type Event struct {
	Kind    EventKind
	Payload string
	Effects []Effect
}

// Effect is one proposed mutation in the form the plan builder consumes
// (OpenAPI PlanEffect: type, path, diff).
type Effect struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Diff string `json:"diff,omitempty"`
}
