// Package tracker defines the Provider port for work trackers.
//
// Issues, comments, and status changes that Zeroth must read or write go
// through this port. Linear is the stage-1 provider; the interface exists so
// the kernel does not take a vendor dependency.
package tracker
