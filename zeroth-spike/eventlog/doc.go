// Package eventlog is the spike's append-only SQLite session event log.
//
// The log is the source of truth for a session. The WebSocket stream is
// a live tail of these rows, not a second channel. Attach is replay
// from this table, then tail. WAL mode is required so readers do not
// block the writer while a session is streaming.
package eventlog
