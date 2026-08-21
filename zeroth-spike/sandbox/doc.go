// Package sandbox is the spike Driver port for isolated agent work.
//
// Concrete runtimes implement Driver. The spike ships a memory driver
// that unpacks fixture tars so later gates can measure ingest. The
// docker driver is named and interface-checked here; launching real
// containers is a later gate, not this setup. This shape is intended
// to survive into M1 and M2 even if these implementations do not.
package sandbox
