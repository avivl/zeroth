// Package session is the spike session state machine and its event log.
//
// A session is one human-supervised run. The machine records every
// transition. The event log is the source of truth for what happened,
// not a best-effort sidecar. This shape is intended to survive into
// M1 and M2 even if the in-memory implementation here does not.
package session
