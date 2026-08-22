// Package server is the local HTTP surface for zerothd.
//
// It implements the generated OpenAPI ServerInterface for the run
// lifecycle the CLI and web UI share: create, list, get, steer,
// background, foreground, GET /runs/{id}/events, audit list/verify,
// agent PATCH (a signed audited action), automatic plan cross-exam,
// GET /plans, plan approve / request-changes / branch / apply,
// approvals, memory, checkpoints, leases, and
// GET /agents/{id}/cross-exam-stats. Stop remains 501 until the
// session machine grows a cancelled terminal. The daemon wires this
// package to the store, signer, and session supervisor; it does not
// talk to Docker, Claude Code, or Linear by name.
package server
