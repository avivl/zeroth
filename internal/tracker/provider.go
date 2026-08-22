package tracker

import "context"

// Provider is an issue tracker.
//
// A Provider is a port. Concrete trackers (Linear, and later others)
// implement this interface; zerothd depends on the port, not the vendor.
// Guarantees below are the conformance contract. A second provider must
// pass the same suite unchanged except for adding its table row (Z1-071,
// ADR-Z-0006).
type Provider interface {
	// Name is a stable identifier used in logs, audit records, and
	// conformance tests (for example "linear").
	Name() string

	// Capabilities reports optional features. Cycles and milestones are
	// flags, not assumed methods, so a tracker without them still fits.
	Capabilities() Capabilities

	// GetIssue loads one issue by key (human id or vendor id). Unknown
	// keys are ErrNotFound. Empty key is ErrInvalid.
	GetIssue(ctx context.Context, key string) (Issue, error)

	// Comment posts markdown body on the issue. Empty key or body is
	// ErrInvalid. The body is what operators read in the tracker; plan
	// diffs belong in collapsed details (FormatPlanComment).
	Comment(ctx context.Context, key, body string) (CommentRef, error)

	// SetState moves the issue to state. Empty key or Kind is ErrInvalid.
	SetState(ctx context.Context, key string, state State) error

	// Assignments yields assign-to-Zeroth edges until ctx is cancelled,
	// then the channel closes. Assigned starts a headless run.
	// Unassigned cancels that run. The first snapshot emits Assigned
	// for issues already on the agent so a restarted daemon resumes.
	// Stage 1 drives this by polling; webhooks are opt-in configuration.
	Assignments(ctx context.Context) (<-chan AssignmentEvent, error)

	// LinkArtifact attaches a URL (PR, transcript, audit) to the issue.
	// Empty key, URL, or Kind is ErrInvalid.
	LinkArtifact(ctx context.Context, key string, a Artifact) error

	// Unassign removes the agent identity from the issue (classic
	// assignee and native delegate) so a later assignment can start a
	// fresh run. Empty key is ErrInvalid. An issue that is not on the
	// agent is a no-op, not an error.
	Unassign(ctx context.Context, key string) error
}
