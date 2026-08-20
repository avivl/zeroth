// Package session is the session state machine and its event log.
//
// A session is one human-supervised run of one or more agents. The machine
// records every transition; the event log is the source of truth for what
// happened, not a best-effort sidecar.
package session
