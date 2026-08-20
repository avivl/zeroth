// Package store defines the Store port for durable state.
//
// Sessions, plans, grants, leases, and audit records persist through this
// port. SQLite is the stage-1 implementation because stage 1 is local and
// single-player.
package store
