// Package session is the session state machine and its event log.
//
// A session is one human-supervised run of one or more agents. The append-only
// event log is the source of truth for what happened, not a best-effort
// sidecar and not a second in-memory copy of status. Current status is
// derived by replaying the log. The live stream is a tail of the same log:
// attach is replay, then live tail. Promotion and demotion only add or
// remove listeners, so they cannot lose history.
//
// Lifecycle is pending, running, awaiting-approval, applying, then done or
// failed. Attachment (attached vs background) is orthogonal to lifecycle.
// Illegal transitions return an error; they do not no-op. Every accepted
// transition appends an event, so a later replay reconstructs the same
// state. A supervisor goroutine per live session serializes mutations
// (steer, attach, background, and the rest) so concurrent listeners race
// only on the log, which is the log's job.
package session
