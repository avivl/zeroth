package sandbox

import (
	"context"
	"fmt"
	"io"

	"github.com/avivl/zeroth/zeroth-spike/session"
)

// HandleID identifies a running sandbox instance. It is a distinct
// named type, not a string and not interchangeable with session.ID.
type HandleID struct {
	raw string
}

// ParseHandleID returns a HandleID from a non-empty raw value.
func ParseHandleID(raw string) (HandleID, error) {
	if raw == "" {
		return HandleID{}, fmt.Errorf("sandbox handle id: empty")
	}
	return HandleID{raw: raw}, nil
}

// String returns the raw identifier.
func (id HandleID) String() string { return id.raw }

// IsZero reports whether id is the zero value.
func (id HandleID) IsZero() bool { return id.raw == "" }

// Workspace is a fixture tree packed as an uncompressed tar.
type Workspace struct {
	TarPath string
}

// StartRequest is what a Driver needs to isolate one session's work.
type StartRequest struct {
	SessionID session.ID
	Workspace Workspace
	// Egress is the per-destination allowlist derived from active
	// leases. Empty means deny all. The docker driver uses
	// --network none in that case.
	Egress Allowlist
}

// ExecResult is the outcome of one command inside an instance.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Snapshot is a checkpoint: a workspace tar plus the session
// transcript. It is not a frozen process. Kill drops in-flight
// PIDs; restore starts a new sandbox from this pair.
type Snapshot struct {
	TarPath    string
	Transcript []session.Event
}

// Instance is one isolated workspace.
type Instance interface {
	ID() HandleID
	SessionID() session.ID
	Exec(ctx context.Context, argv []string) (ExecResult, error)
	// ExportTar writes an uncompressed tar of the workspace overlay.
	// The read is from the host mount, so it can run alongside Exec
	// and does not wait for an in-flight turn to finish.
	ExportTar(ctx context.Context, w io.Writer) error
	// ImportTar replaces the workspace tree with the tar contents.
	ImportTar(ctx context.Context, r io.Reader) error
	// Kill SIGKILLs the sandbox. The overlay remains until Stop so a
	// last ExportTar can still run. Processes are not checkpointed.
	Kill(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Driver isolates agent work from the host.
//
// A Driver is a port. zerothd will depend on this shape, not on a
// runtime. Name is a stable identifier used in logs, audit records,
// and tests (for example "docker" or "memory").
type Driver interface {
	Name() string
	Start(ctx context.Context, req StartRequest) (Instance, error)
}
