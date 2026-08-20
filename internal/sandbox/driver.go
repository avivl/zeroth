package sandbox

// Driver isolates agent work from the host.
//
// A Driver is a port. Concrete runtimes (Docker, and later others) implement
// this interface; zerothd depends on the port, not the runtime.
type Driver interface {
	// Name is a stable identifier used in logs, audit records, and
	// conformance tests (for example "docker").
	Name() string
}
