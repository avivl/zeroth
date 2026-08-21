// Package sandbox is the spike Driver port for isolated agent work.
//
// Concrete runtimes implement Driver. The docker implementation uses
// an overlay workspace, ExportTar / ImportTar, Exec, and Kill. A
// checkpoint is that tar plus the session transcript, not a frozen
// process. The memory driver unpacks tars without isolation so tests
// can exercise the interface without Docker. This shape is intended
// to survive into M1 and M2 even if these implementations do not.
package sandbox
