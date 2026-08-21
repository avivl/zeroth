package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/avivl/zeroth/internal/sandbox"
)

// Exec implements [sandbox.Driver].
func (d *Driver) Exec(ctx context.Context, id sandbox.ID, cmd sandbox.Cmd) (sandbox.ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", err)
	}
	if len(cmd.Argv) == 0 {
		return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", sandbox.ErrInvalid)
	}
	for _, e := range cmd.Env {
		if !strings.Contains(e, "=") {
			return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", sandbox.ErrInvalid)
		}
	}
	for _, f := range cmd.Files {
		if err := sandbox.ValidCredPath(f.Path); err != nil {
			return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", err)
		}
	}
	inst, err := d.lookup(id)
	if err != nil {
		return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", err)
	}

	inst.mu.Lock()
	container := inst.container
	stopped := inst.stopped
	killed := inst.killed
	proxyURL := ""
	if inst.proxy != nil {
		proxyURL = inst.proxy.URL()
	}
	inst.mu.Unlock()
	if stopped {
		return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", sandbox.ErrStopped)
	}
	if killed || container == "" {
		return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", sandbox.ErrKilled)
	}

	cleanup, err := stageCredFiles(ctx, container, cmd.Files)
	if err != nil {
		return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", err)
	}
	defer cleanup()

	args := []string{"exec", "-w", "/workspace"}
	if proxyURL != "" {
		args = append(args,
			"-e", "HTTP_PROXY="+proxyURL,
			"-e", "HTTPS_PROXY="+proxyURL,
			"-e", "http_proxy="+proxyURL,
			"-e", "https_proxy="+proxyURL,
			"-e", "NO_PROXY=localhost",
			"-e", "no_proxy=localhost",
		)
	}
	for _, e := range cmd.Env {
		args = append(args, "-e", e)
	}
	args = append(args, container)
	args = append(args, cmd.Argv...)

	c := exec.CommandContext(ctx, "docker", args...)
	stdout, stderr, err := stdoutStderr(c)
	res := sandbox.ExecResult{Stdout: stdout, Stderr: stderr}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		if ctx.Err() != nil {
			return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", ctx.Err())
		}
		return sandbox.ExecResult{}, fmt.Errorf("sandbox docker exec: %w", err)
	}
	return res, nil
}
