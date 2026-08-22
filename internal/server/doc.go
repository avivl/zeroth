// Package server is the local HTTP surface for zerothd.
//
// It implements the generated OpenAPI ServerInterface for the run
// lifecycle the CLI and web UI share: create, list, get, steer,
// background, foreground, GET /runs/{id}/events, audit list/verify,
// agent PATCH (a signed audited action), automatic plan cross-exam,
// GET /plans, GET /agents/{id}/cross-exam-stats, and memory notebook
// write plus proposal accept/reject. Sandbox spawn compiles the notebook
// slice into AGENTS.md before the worker starts (Z1-118). A plan
// memory_proposal row is applied as Notebook.Propose, never as a direct
// write (Z1-022). Other contract paths return 501 until their packages
// land. The daemon wires this package to the store, signer, session
// supervisor, tracker.Provider, and sandbox.Driver; it does not import
// Linear or Docker by name. Assign-to-Zeroth is the tracker watch loop:
// Assigned starts a headless run, Unassigned fails it and stops the
// sandbox.
package server
