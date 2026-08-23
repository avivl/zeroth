// Package tracker defines the Provider port for work trackers.
//
// Issues, comments, and status changes that Zeroth must read or write go
// through this port. Linear is the stage-1 provider; the interface exists so
// the kernel does not take a vendor dependency (ADR-Z-0006). Assigning an
// issue to the agent identity starts a headless run; un-assigning cancels
// that run and must stop the sandbox (Z1-038). ListComments is how a
// new run sees operator rejections posted on the issue. Unassign is
// also the cleanup half of retract: after a bad PR is closed, the
// agent is removed so a later assignment can start a fresh run
// (Linear 42-56). Polling is the stage-1 default so no inbound network
// path is required (Z1-082).
package tracker
