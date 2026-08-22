// Package server is the local HTTP surface for zerothd.
//
// It implements the generated OpenAPI ServerInterface for the run
// lifecycle the CLI and web UI share: create, list, get, steer,
// background, foreground, GET /runs/{id}/events, audit list/verify,
// agent PATCH (a signed audited action), automatic plan cross-exam,
// GET /plans, plan approve / request-changes / branch / apply,
// approvals, checkpoints, leases, GET /agents/{id}/cross-exam-stats,
// and memory notebook write plus proposal accept/reject. Sandbox spawn
// copies the operator's local checkout into the overlay (when configured)
// and compiles the notebook slice into AGENTS.md before the worker starts
// (Z1-118). Draft observation hashes files from that overlay; a missing
// modify/destroy target fails with a workspace-observe error rather than
// an opaque plan-builder rejection. Apply rechecks those hashes against
// the live overlay, patches modify rows onto existing files (never a
// silent full-file overwrite), rechecks the recorded postcondition
// hash, then commits, pushes, and opens a GitHub pull request so the
// tracker completion comment can link it. A plan memory_proposal row is applied as Notebook.Propose,
// never as a direct write (Z1-022). Stop remains 501 until the session
// machine grows a cancelled terminal. The daemon wires this package to
// the store, signer, session supervisor, tracker.Provider,
// sandbox.Driver, and harness.Driver; it does not import Linear, Docker,
// or Claude Code by name. Assign-to-Zeroth is the tracker watch loop:
// Assigned starts a headless run, Unassigned fails it and stops the
// sandbox. The worker drives one harness plan-generation attempt per
// draft. Request-changes posts the operator comment on the tracker
// issue, appends it to the run prompt, and starts another attempt on
// the same run. A new assign also reads the issue comment thread so
// the correction survives un-assign. A run that produces no draft
// fails instead of completing.
// Draft attaches the plan id to the session row before proposing;
// status sync must not clear that id.
package server
