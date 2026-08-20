package harness

// Driver drives an agent runtime through a session.
//
// A Driver is a port. Concrete harnesses implement this interface;
// zerothd depends on the port, not the vendor.
type Driver interface {
	// Name is a stable identifier used in logs, audit records, and
	// conformance tests (for example "claudecode").
	Name() string
}
