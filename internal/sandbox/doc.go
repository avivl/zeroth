// Package sandbox defines the Driver port for isolated agent execution.
//
// Implementations live in subpackages (docker) and are proven against
// conformance_test.go rather than ad-hoc tests in the daemon. Adding a
// second backend is one table row; the cases stay implementation-agnostic
// (Z1-080, NFR-4).
//
// Deny by default: Spawn starts with no egress. AllowEgress is the only
// way to open destinations, and empty rules put the sandbox back on deny.
// A checkpoint is a workspace tar (ExportTar / ImportTar), not a frozen
// process. Kill drops in-flight PIDs; the overlay remains until Stop so a
// last ExportTar can still run.
//
// Credentials (Z1-113) are injected per Exec via env or a tmpfs under
// CredsDir. They are never written into /workspace. ExportTar strips a
// hard exclusion list and secret-scans what remains, failing closed on a
// finding. One checkpoint hydrates any number of independent sandboxes.
package sandbox
