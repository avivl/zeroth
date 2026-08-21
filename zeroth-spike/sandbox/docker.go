package sandbox

import (
	"context"
	"fmt"
)

// Docker is the named Docker sandbox driver for the spike.
//
// Setup does not launch containers. Later gates measure real Docker
// ingest and isolation against this same Driver shape.
type Docker struct{}

// NewDocker returns a Docker sandbox driver.
func NewDocker() *Docker { return &Docker{} }

// Name implements [Driver].
func (*Docker) Name() string { return "docker" }

var _ Driver = (*Docker)(nil)

// Start implements [Driver]. The setup stub does not create a
// container. The compose stack is the spike process itself.
func (*Docker) Start(_ context.Context, req StartRequest) (Instance, error) {
	if req.SessionID.IsZero() {
		return nil, fmt.Errorf("sandbox docker start: empty session id")
	}
	return nil, fmt.Errorf("sandbox docker start: setup stub does not launch containers")
}
