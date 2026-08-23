package sandbox

import (
	"context"
	"io"
)

// Driver isolates agent work from the host.
//
// A Driver is a port. Concrete runtimes (Docker, and later others)
// implement this interface; zerothd depends on the port, not the runtime.
// Guarantees below are the conformance contract. They come from the BA-6
// spike divergence log (G2/G3) so a second backend cannot drift on the
// semantics that actually matter.
type Driver interface {
	// Name is a stable identifier used in logs, audit records, and
	// conformance tests (for example "docker").
	Name() string

	// Spawn creates an isolated sandbox from spec and returns a handle.
	// Egress starts denied. The workspace is spec.Workspace unpacked at
	// /workspace, or empty if Workspace is nil. The sandbox cannot
	// mutate host paths: a write to an absolute host path from inside
	// is a different file (or a missing path), not the host's.
	Spawn(ctx context.Context, spec Spec) (Sandbox, error)

	// Exec runs cmd inside the sandbox.
	//
	// Non-zero process exit is ExecResult.ExitCode with a nil error.
	// Failures of the driver itself (unknown id, killed or stopped
	// sandbox, runtime down) are errors. Env and Files in cmd are
	// visible to that process; the host environment is not. Files
	// land on a tmpfs, never /workspace. Empty Argv is ErrInvalid.
	Exec(ctx context.Context, id ID, cmd Cmd) (ExecResult, error)

	// ExportTar writes an uncompressed tar of the /workspace tree.
	// Byte-identity of a checkpoint is the content hash of that tree
	// (paths, modes, file bytes), not the tar file: GNU headers carry
	// mtime and owners that change across a round-trip. Export does
	// not take a turn lock; it may run alongside Exec. A live tar can
	// be slightly inconsistent if the command writes during the walk.
	// ExportTar remains valid after Kill until Stop.
	//
	// Paths matching ExcludedFromExport are omitted. The remaining
	// tree is secret-scanned; a finding is ErrSecret and w receives
	// no bytes (fail closed, Z1-113). The same tar hydrates any
	// number of independent sandboxes (branch).
	ExportTar(ctx context.Context, id ID, w io.Writer) error

	// ImportTar replaces the /workspace tree with the tar contents.
	// Paths that escape the workspace are ErrInvalid. Only /workspace
	// is in the checkpoint: writes under /tmp (tmpfs on a read-only
	// rootfs) do not survive a restore.
	ImportTar(ctx context.Context, id ID, r io.Reader) error

	// AllowEgress replaces the sandbox's egress allowlist. Empty or
	// nil rules are deny-all (no network except loopback). A listed
	// destination is allowed through the driver's enforcement point
	// (stage 1: HTTP/HTTPS CONNECT proxy via HTTP_PROXY). A destination
	// that is not listed is denied. Clients that ignore the proxy are
	// out of scope for stage 1. AllowEgress does not read policy; the
	// caller passes facts in. If deny-all cannot be established, the
	// call returns an error rather than reporting success while
	// unrestricted egress may still be possible.
	AllowEgress(ctx context.Context, id ID, rules []EgressRule) error

	// Kill SIGKILLs in-flight work. Processes are not checkpointed.
	// The workspace overlay remains until Stop so a last ExportTar can
	// still run. Work written after the last export is gone on restore.
	// An in-flight Exec returns either a driver error or a non-zero
	// ExitCode (signal). Subsequent Exec returns ErrKilled. Kill is
	// idempotent. A restored pid file is not liveness: that number can
	// name a different process in a new pid namespace. An in-sandbox
	// daemon's files restore; the process does not.
	Kill(ctx context.Context, id ID) error

	// Stop releases host resources (runtime, overlay, temp dirs).
	// The id is then unknown: further Exec, ExportTar, ImportTar,
	// AllowEgress, and Kill return ErrNotFound (or ErrStopped). Stop
	// on an already-released id returns nil.
	Stop(ctx context.Context, id ID) error
}
