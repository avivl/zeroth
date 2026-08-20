package linear

import "github.com/avivl/zeroth/internal/tracker"

// Provider is a Linear tracker. The skeleton implements only the port identity.
type Provider struct{}

// New returns a Linear tracker provider.
func New() *Provider {
	return &Provider{}
}

// Name implements [tracker.Provider].
func (*Provider) Name() string { return "linear" }

var _ tracker.Provider = (*Provider)(nil)
