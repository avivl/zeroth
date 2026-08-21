package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"

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
		// Write from inside the container so a --read-only rootfs still
		// accepts bytes on the credential tmpfs. docker cp targets the
		// rootfs and fails with "marked read-only".
		cmd := exec.CommandContext(ctx, "docker", "exec", "-i", container,
			"sh", "-c", `cat >"$1" && chmod 0600 "$1"`, "zeroth-cred", f.Path)
		cmd.Stdin = bytes.NewReader(f.Data)
		out, err = cmd.CombinedOutput()
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("write cred: %s", bytesHead(out, err))
		}
		staged = append(staged, f.Path)
	}
	return cleanup, nil
}
