package tracker

import "time"

// Capabilities names optional tracker features. A provider without cycles
// or milestones still implements [Provider]; callers must not assume either
// flag is true (GitHub Issues will not have them).
type Capabilities struct {
	Cycles     bool
	Milestones bool
}

// StateKind is a vendor-neutral workflow kind. Names match common tracker
// types so Linear can map 1:1 and a later GitHub Issues provider can fold
// open/closed onto unstarted/completed.
type StateKind string

const (
	StateBacklog   StateKind = "backlog"
	StateUnstarted StateKind = "unstarted"
	StateStarted   StateKind = "started"
	StateCompleted StateKind = "completed"
	StateCanceled  StateKind = "canceled"
)

// State is a workflow position. Kind is required for [Provider.SetState].
// Name is an optional provider-specific label ("In Progress").
type State struct {
	Kind StateKind
	Name string
}

// Issue is one work item as the port sees it.
type Issue struct {
	// Key is the human identifier (for example "42-29"). It is what
	// callers pass to GetIssue, Comment, SetState, LinkArtifact, and Unassign.
	Key string
	// ID is the vendor's opaque id. Drivers may use it internally.
	ID          string
	Title       string
	Description string
	URL         string
	AssigneeID  string
	DelegateID  string
	State       State
	Project     string
}

// Comment is one item on an issue thread as the port sees it.
type Comment struct {
	ID     string
	Body   string
	URL    string
	Author string
	At     time.Time
	// Bot is true when the vendor marks the author as an app or agent
	// identity rather than a human operator.
	Bot bool
}

// CommentRef is the handle a successful Comment returns.
type CommentRef struct {
	ID  string
	URL string
}

// ArtifactKind names what LinkArtifact is attaching. Cost and the audit
// summary are usually comment text; PR and transcript are URLs.
type ArtifactKind string

const (
	ArtifactPR         ArtifactKind = "pr"
	ArtifactTranscript ArtifactKind = "transcript"
	ArtifactAudit      ArtifactKind = "audit"
	ArtifactCost       ArtifactKind = "cost"
)

// Artifact is a URL (and title) attached to an issue on completion so
// someone reading only the tracker can reconstruct the run.
type Artifact struct {
	Kind  ArtifactKind
	URL   string
	Title string
}

// AssignmentKind is assigned or unassigned.
type AssignmentKind string

const (
	// Assigned means the issue is now assigned to the Zeroth agent identity.
	Assigned AssignmentKind = "assigned"
	// Unassigned means the issue left that identity. The run must cancel
	// and the sandbox must actually stop (Z1-038).
	Unassigned AssignmentKind = "unassigned"
)

// AssignmentEvent is one assign-to-Zeroth edge. Polling is the stage-1
// default so no inbound network path is required (Z1-082).
type AssignmentEvent struct {
	Kind  AssignmentKind
	Key   string
	Issue Issue
	At    time.Time
}

// Completion is what FormatCompletion renders onto the issue when a run
// finishes. Empty URL fields are omitted rather than invented.
type Completion struct {
	RunID       string
	Cost        string
	Transcript  string
	PullRequest string
	Audit       string
}

// Retract is what FormatRetractComment renders onto the issue when a
// run's already-produced output is disavowed.
type Retract struct {
	RunID       string
	Reason      string
	PullRequest string
	Closed      bool
}
