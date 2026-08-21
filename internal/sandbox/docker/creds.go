package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"

	"github.com/avivl/zeroth/internal/sandbox"
)

func credsTmpfsSpec() string {
	spec := sandbox.CredsDir + ":rw,exec,mode=0770"
	uid := os.Getuid()
	gid := os.Getgid()
	if uid != 0 {
		spec += fmt.Sprintf(",uid=%d,gid=%d", uid, gid)
	}
	return spec
}

func stageCredFiles(ctx context.Context, container string, files []sandbox.CredFile) (func(), error) {
	if len(files) == 0 {
		return func() {}, nil
	}
	staged := make([]string, 0, len(files))
	cleanup := func() {
		for _, p := range staged {
			_ = exec.Command("docker", "exec", container, "rm", "-f", "--", p).Run()
		}
	}
	for _, f := range files {
		if err := sandbox.ValidCredPath(f.Path); err != nil {
			cleanup()
			return nil, fmt.Errorf("%w", err)
		}
		parent := path.Dir(f.Path)
		out, err := exec.CommandContext(ctx, "docker", "exec", container, "mkdir", "-p", "--", parent).CombinedOutput()
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("mkdir creds: %s", bytesHead(out, err))
		}
		host, err := os.CreateTemp("", "zeroth-cred-")
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("cred temp: %w", err)
		}
		hostName := host.Name()
		if _, err := host.Write(f.Data); err != nil {
			_ = host.Close()
			_ = os.Remove(hostName)
			cleanup()
			return nil, fmt.Errorf("cred temp write: %w", err)
		}
		if err := host.Chmod(0o600); err != nil {
			_ = host.Close()
			_ = os.Remove(hostName)
			cleanup()
			return nil, fmt.Errorf("cred temp chmod: %w", err)
		}
		if err := host.Close(); err != nil {
			_ = os.Remove(hostName)
			cleanup()
			return nil, fmt.Errorf("cred temp close: %w", err)
		}
		cpOut, err := exec.CommandContext(ctx, "docker", "cp", hostName, container+":"+f.Path).CombinedOutput()
		_ = os.Remove(hostName)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("docker cp cred: %s", bytesHead(cpOut, err))
		}
		if err := chownCred(ctx, container, f.Path); err != nil {
			cleanup()
			return nil, err
		}
		staged = append(staged, f.Path)
	}
	return cleanup, nil
}

func chownCred(ctx context.Context, container, dest string) error {
	uid := os.Getuid()
	gid := os.Getgid()
	if uid == 0 {
		out, err := exec.CommandContext(ctx, "docker", "exec", container, "chmod", "0600", "--", dest).CombinedOutput()
		if err != nil {
			return fmt.Errorf("chmod cred: %s", bytesHead(out, err))
		}
		return nil
	}
	out, err := exec.CommandContext(ctx, "docker", "exec", "-u", "0", container,
		"chown", strconv.Itoa(uid)+":"+strconv.Itoa(gid), "--", dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("chown cred: %s", bytesHead(out, err))
	}
	out, err = exec.CommandContext(ctx, "docker", "exec", "-u", "0", container,
		"chmod", "0600", "--", dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("chmod cred: %s", bytesHead(out, err))
	}
	return nil
}
