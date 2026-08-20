package claudecode

import "github.com/avivl/zeroth/internal/harness"

// Driver is a Claude Code harness. The skeleton implements only the port identity.
type Driver struct{}

// New returns a Claude Code harness driver.
func New() *Driver {
	return &Driver{}
}

// Name implements [harness.Driver].
func (*Driver) Name() string { return "claudecode" }

var _ harness.Driver = (*Driver)(nil)
