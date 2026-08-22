// Package harness defines the Driver port for agent runtimes.
//
// A harness is the thing that actually runs the model-and-tools loop
// (Claude Code, and later others). Zeroth does not embed a proprietary
// agent; it drives one through this port. The five methods (Start,
// Stream, Steer, Checkpoint, Stop) are the contract a later adapter
// must satisfy unchanged except for adding its table row in
// conformance_test.go (Z1-006, NFR-4, ADR-Z-0003).
//
// The agent proposes effects. It does not apply them. Apply is the
// plan lifecycle, not a harness shortcut. Stage 1's Claude Code adapter
// is a host subprocess against the overlay (ADR-Z-0010), not sandbox.Exec.
package harness
