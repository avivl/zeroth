package sandbox

import (
	"context"
	"fmt"

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
}

// ExecResult is the outcome of one command inside an instance.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Instance is one isolated workspace.
type Instance interface {
	ID() HandleID
	SessionID() session.ID
	Exec(ctx context.Context, argv []string) (ExecResult, error)
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
