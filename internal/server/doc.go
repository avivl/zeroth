// Package server is the local HTTP surface for zerothd.
//
// It implements the generated OpenAPI ServerInterface for the run
// lifecycle the CLI and web UI share: create, list, get, steer,
// background, foreground, GET /runs/{id}/events, audit list/verify,
// agent PATCH (a signed audited action), automatic plan cross-exam,
// GET /plans, plan approve / request-changes / branch / apply,
// approvals, checkpoints, leases, GET /agents/{id}/cross-exam-stats,
// and memory notebook write plus proposal accept/reject. Sandbox spawn
// compiles the notebook slice into AGENTS.md before the worker starts
// (Z1-118). A plan memory_proposal row is applied as Notebook.Propose,
// never as a direct write (Z1-022). Stop remains 501 until the session
// machine grows a cancelled terminal. The daemon wires this package to
// the store, signer, session supervisor, tracker.Provider, and
// sandbox.Driver; it does not import Linear or Docker by name.
// Assign-to-Zeroth is the tracker watch loop: Assigned starts a
// headless run, Unassigned fails it and stops the sandbox.
package server
