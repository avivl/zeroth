// Package store defines the Store port for durable state.
//
// Sessions, events, plans, approvals, memory, audit records, leases,
// checkpoint index rows, and agents persist through this port. SQLite is the
// stage-1 implementation because stage 1 is local and single-player.
//
// Callers depend on [Store], never on a backend by name. A later Postgres
// driver must pass conformance_test.go unchanged except for adding its row
// to the implementation table (ADR-Z-0004, NFR-4).
package store
