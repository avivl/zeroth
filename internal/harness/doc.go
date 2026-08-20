// Package harness defines the Driver port for agent runtimes.
//
// A harness is the thing that actually runs the model-and-tools loop
// (Claude Code, and later others). Zeroth does not embed a proprietary
// agent; it drives one through this port.
package harness
