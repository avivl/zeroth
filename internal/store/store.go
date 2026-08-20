package store

// Store is durable state for the control plane.
//
// A Store is a port. Concrete backends (SQLite, and later others) implement
// this interface; zerothd depends on the port, not the engine.
type Store interface {
	// Name is a stable identifier used in logs, audit records, and
	// conformance tests (for example "sqlite").
	Name() string
	// Close releases backend resources.
	Close() error
}
