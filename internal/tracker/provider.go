package tracker

// Provider is an issue tracker.
//
// A Provider is a port. Concrete trackers (Linear, and later others)
// implement this interface; zerothd depends on the port, not the vendor.
type Provider interface {
	// Name is a stable identifier used in logs, audit records, and
	// conformance tests (for example "linear").
	Name() string
}
