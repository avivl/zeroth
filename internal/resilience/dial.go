package resilience

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// DialUnix is the worked Failsafe-go call site: a real unix-socket dial with
// retry and timeout. zerothd uses it to probe the Docker socket at startup.
// Sandbox, harness, and tracker drivers should reuse NewExecutor, Run, and Get,
// not this dial, when they grow real remotes.
func DialUnix(ctx context.Context, socket string, opts Options) error {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return fmt.Errorf("resilience dial unix: empty socket path")
	}
	exec := NewExecutor[struct{}](opts)
	err := Run(ctx, exec, func(ctx context.Context) error {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", socket)
		if err != nil {
			return fmt.Errorf("dial unix %s: %w", socket, err)
		}
		if err := conn.Close(); err != nil {
			return fmt.Errorf("close unix %s: %w", socket, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("resilience dial unix %s: %w", socket, err)
	}
	return nil
}
