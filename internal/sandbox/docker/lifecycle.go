package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/avivl/zeroth/internal/resilience"
	"github.com/avivl/zeroth/internal/sandbox"
)

// Spawn implements [sandbox.Driver].
func (d *Driver) Spawn(ctx context.Context, spec sandbox.Spec) (sandbox.Sandbox, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.Sandbox{}, fmt.Errorf("sandbox docker spawn: %w", err)
	}
	if err := d.ensureImage(ctx); err != nil {
		return sandbox.Sandbox{}, fmt.Errorf("sandbox docker spawn: %w", err)
	}

	id, err := sandbox.NewID()
	if err != nil {
		return sandbox.Sandbox{}, fmt.Errorf("sandbox docker spawn: %w", err)
	}
	dir, err := os.MkdirTemp(d.root, "zeroth-sbx-"+id.String()+"-")
	if err != nil {
		return sandbox.Sandbox{}, fmt.Errorf("sandbox docker spawn: temp dir: %w", err)
	}
	if spec.Workspace != nil {
		if err := sandbox.UnpackOverlay(dir, spec.Workspace); err != nil {
			_ = os.RemoveAll(dir)
			return sandbox.Sandbox{}, fmt.Errorf("sandbox docker spawn: %w", err)
		}
	}

	inst := &instance{id: id, workspace: dir}
	if err := inst.createContainer(ctx, d.image); err != nil {
		_ = os.RemoveAll(dir)
		return sandbox.Sandbox{}, fmt.Errorf("sandbox docker spawn: %w", err)
	}

	d.mu.Lock()
	d.inst[id.String()] = inst
	d.mu.Unlock()
	return sandbox.Sandbox{ID: id}, nil
}

func (d *Driver) ensureImage(ctx context.Context) error {
	inspect := exec.CommandContext(ctx, "docker", "image", "inspect", d.image)
	if err := inspect.Run(); err == nil {
		return nil
	}
	err := resilience.Run(ctx, d.execer, func(ctx context.Context) error {
		out, err := exec.CommandContext(ctx, "docker", "pull", d.image).CombinedOutput()
		if err != nil {
			return fmt.Errorf("pull %s: %s", d.image, bytesHead(out, err))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("sandbox docker image: %w", err)
	}
	return nil
}

func (i *instance) createContainer(ctx context.Context, image string) error {
	name := fmt.Sprintf("zeroth-sbx-%s-%d", i.id.String(), time.Now().UnixNano())
	args := []string{
		"create",
		"--name", name,
		"--read-only",
		"--tmpfs", "/tmp:rw,exec",
		"--mount", "type=bind,src=" + i.workspace + ",dst=/workspace",
		"--workdir", "/workspace",
		"--tmpfs", credsTmpfsSpec(),
		"--network", "none",
		"--add-host", "host.docker.internal:host-gateway",
	}
	if spec := currentUserSpec(); spec != "" {
		args = append(args, "--user", spec)
	}
	args = append(args, image, "sleep", "infinity")
	create := exec.CommandContext(ctx, "docker", args...)
	out, err := create.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create: %s", bytesHead(out, err))
	}
	i.container = name
	start := exec.CommandContext(ctx, "docker", "start", name)
	if out, err := start.CombinedOutput(); err != nil {
		_ = exec.Command("docker", "rm", "-f", name).Run()
		i.container = ""
		return fmt.Errorf("start: %s", bytesHead(out, err))
	}
	return nil
}

// Kill implements [sandbox.Driver].
func (d *Driver) Kill(ctx context.Context, id sandbox.ID) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker kill: %w", err)
	}
	inst, err := d.lookup(id)
	if err != nil {
		return fmt.Errorf("sandbox docker kill: %w", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.stopped {
		return fmt.Errorf("sandbox docker kill: %w", sandbox.ErrStopped)
	}
	if inst.killed || inst.container == "" {
		inst.killed = true
		return nil
	}
	out, err := exec.CommandContext(ctx, "docker", "kill", inst.container).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox docker kill: %s", bytesHead(out, err))
	}
	inst.killed = true
	return nil
}

// Stop implements [sandbox.Driver].
func (d *Driver) Stop(ctx context.Context, id sandbox.ID) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker stop: %w", err)
	}
	inst, err := d.lookup(id)
	if errors.Is(err, sandbox.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sandbox docker stop: %w", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.stopped {
		return nil
	}
	inst.stopped = true
	inst.killed = true
	err = inst.cleanupLocked()
	d.mu.Lock()
	delete(d.inst, id.String())
	d.mu.Unlock()
	if err != nil {
		return fmt.Errorf("sandbox docker stop: %w", err)
	}
	return nil
}

func (i *instance) cleanupLocked() error {
	var first error
	if i.container != "" {
		out, err := exec.Command("docker", "rm", "-f", i.container).CombinedOutput()
		if err != nil && first == nil {
			first = fmt.Errorf("rm: %s", bytesHead(out, err))
		}
		i.container = ""
	}
	if i.proxy != nil {
		if err := i.proxy.Close(); err != nil && first == nil {
			first = fmt.Errorf("proxy: %w", err)
		}
		i.proxy = nil
	}
	if i.workspace != "" {
		if err := forceRemoveAll(i.workspace); err != nil && first == nil {
			first = fmt.Errorf("cleanup: %w", err)
		}
	}
	return first
}

// forceRemoveAll deletes dir even when the container wrote files as root.
func forceRemoveAll(dir string) error {
	if err := os.RemoveAll(dir); err == nil {
		return nil
	}
	out, err := exec.Command("docker", "run", "--rm", "--network", "none",
		"--mount", "type=bind,src="+dir+",dst=/wipe",
		defaultImage, "sh", "-c", "find /wipe -mindepth 1 -delete").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", bytesHead(out, err))
	}
	return os.RemoveAll(dir)
}
