package docker

import "github.com/avivl/zeroth/internal/sandbox"

// Driver is a Docker sandbox. The skeleton implements only the port identity.
type Driver struct{}

// New returns a Docker sandbox driver.
func New() *Driver {
	return &Driver{}
}

// Name implements [sandbox.Driver].
func (*Driver) Name() string { return "docker" }

var _ sandbox.Driver = (*Driver)(nil)
