package sandbox

import "io"

// Spec is what Spawn needs to isolate one sandbox.
type Spec struct {
	// Workspace is an uncompressed tar unpacked at /workspace.
	// Nil or an empty tar means an empty directory. Paths that
	// escape the workspace are rejected with ErrInvalid.
	Workspace io.Reader
}

// Sandbox is the handle Spawn returns. Subsequent calls take ID.
type Sandbox struct {
	ID ID
}

// Cmd is one command to run inside a sandbox.
type Cmd struct {
	// Argv is the executable and its arguments. Empty is ErrInvalid.
	Argv []string
	// Env is KEY=value pairs visible only to this command. The host
	// process environment is not forwarded. Malformed entries (no
	// '=') are ErrInvalid.
	Env []string
}

// ExecResult is the outcome of one command. A non-zero process exit is
// ExitCode with a nil error. Driver failures (unknown id, killed
// container, daemon down) are errors.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// EgressRule is one host:port a sandbox may reach. Host matching is
// case-insensitive and literal: an IP does not satisfy a hostname.
// Port 0 means 80 and 443.
type EgressRule struct {
	Host string
	Port int
}
