// Package supervisor drives a spike session: state machine plus agent.
//
// The SQLite event log is the source of truth. The agent (fake ticker or
// a subprocess) only emits tokens; it does not own session state.
package supervisor
