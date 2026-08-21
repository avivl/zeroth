package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/session"
)

const defaultImage = "debian:bookworm-slim"

// Docker is the Docker sandbox driver for the spike. Workspaces live
// on an overlay (kernel overlayfs, fuse-overlayfs, or a plain dir if
// neither mount is available) bind-mounted at /workspace. Checkpoints
// are that tree packed as a tar, not a CRIU dump.
type Docker struct {
	seq   atomic.Uint64
	root  string
	image string
}

// NewDocker returns a Docker sandbox driver.
func NewDocker() *Docker {
	root := os.Getenv("SPIKE_SANDBOX_ROOT")
	if root == "" {
		root = os.TempDir()
	}
	image := os.Getenv("SPIKE_SANDBOX_IMAGE")
	if image == "" {
		image = defaultImage
	}
	return &Docker{root: root, image: image}
}

// Name implements [Driver].
func (*Docker) Name() string { return "docker" }

var _ Driver = (*Docker)(nil)

// Start unpacks req.Workspace.TarPath into an overlay and launches a
// container with that overlay bind-mounted at /workspace.
func (d *Docker) Start(ctx context.Context, req StartRequest) (Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sandbox docker start: %w", err)
	}
	if req.SessionID.IsZero() {
		return nil, fmt.Errorf("sandbox docker start: empty session id")
	}
	if err := d.ensureImage(ctx); err != nil {
		return nil, fmt.Errorf("sandbox docker start: %w", err)
	}

	n := d.seq.Add(1)
	hid, err := ParseHandleID(fmt.Sprintf("docker-%d", n))
	if err != nil {
		return nil, fmt.Errorf("sandbox docker start: %w", err)
	}
	base, err := os.MkdirTemp(d.root, "zeroth-spike-"+hid.String()+"-")
	if err != nil {
		return nil, fmt.Errorf("sandbox docker start: temp dir: %w", err)
	}
	if err := makeOverlayDirs(base); err != nil {
		_ = os.RemoveAll(base)
		return nil, fmt.Errorf("sandbox docker start: %w", err)
	}

	lower := joinOverlay(base, "lower")
	if req.Workspace.TarPath != "" {
		if err := extractTarFile(ctx, req.Workspace.TarPath, lower); err != nil {
			_ = os.RemoveAll(base)
			return nil, fmt.Errorf("sandbox docker start: %w", err)
		}
	}

	method, err := mountOverlay(base)
	if err != nil {
		_ = os.RemoveAll(base)
		return nil, fmt.Errorf("sandbox docker start: %w", err)
	}

	merged := joinOverlay(base, "merged")
	if method == overlayPlain {
		merged = lower
	}

	inst := &dockerInstance{
		id:        hid,
		sessionID: req.SessionID,
		base:      base,
		lower:     lower,
		upper:     joinOverlay(base, "upper"),
		work:      joinOverlay(base, "work"),
		merged:    merged,
		overlay:   method,
		image:     d.image,
		proxy:     nil,
	}

	if !req.Egress.Empty() {
		px, err := ListenProxy(req.Egress)
		if err != nil {
			_ = inst.cleanup()
			return nil, fmt.Errorf("sandbox docker start: %w", err)
		}
		inst.proxy = px
	}

	if err := inst.createContainer(ctx); err != nil {
		_ = inst.cleanup()
		return nil, fmt.Errorf("sandbox docker start: %w", err)
	}
	return inst, nil
}

func (d *Docker) ensureImage(ctx context.Context) error {
	inspect := exec.CommandContext(ctx, "docker", "image", "inspect", d.image)
	if err := inspect.Run(); err == nil {
		return nil
	}
	pull := exec.CommandContext(ctx, "docker", "pull", d.image)
	if out, err := pull.CombinedOutput(); err != nil {
		return fmt.Errorf("pull %s: %w: %s", d.image, err, bytesHead(out, err))
	}
	return nil
}

type dockerInstance struct {
	id        HandleID
	sessionID session.ID
	base      string
	lower     string
	upper     string
	work      string
	merged    string
	overlay   string
	image     string
	container string
	proxy     *Proxy

	mu      sync.Mutex
	killed  bool
	stopped bool
}

var _ Instance = (*dockerInstance)(nil)

func (i *dockerInstance) ID() HandleID          { return i.id }
func (i *dockerInstance) SessionID() session.ID { return i.sessionID }

func (i *dockerInstance) createContainer(ctx context.Context) error {
	name := fmt.Sprintf("zeroth-spike-%s-%d", i.id.String(), time.Now().UnixNano())
	args := []string{
		"create",
		"--name", name,
		"--read-only",
		"--tmpfs", "/tmp:rw,exec",
		"--mount", "type=bind,src=" + i.merged + ",dst=/workspace",
		"--workdir", "/workspace",
	}
	if i.proxy == nil {
		args = append(args, "--network", "none")
	} else {
		proxyURL := fmt.Sprintf("http://host.docker.internal:%d", i.proxy.Port())
		args = append(args,
			"--add-host", "host.docker.internal:host-gateway",
			"-e", "HTTP_PROXY="+proxyURL,
			"-e", "HTTPS_PROXY="+proxyURL,
			"-e", "http_proxy="+proxyURL,
			"-e", "https_proxy="+proxyURL,
			"-e", "NO_PROXY=localhost,127.0.0.1",
			"-e", "no_proxy=localhost,127.0.0.1",
		)
	}
	if spec := currentUserSpec(); spec != "" {
		args = append(args, "--user", spec)
	}
	args = append(args, i.image, "sleep", "infinity")
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

func (i *dockerInstance) Exec(ctx context.Context, argv []string) (ExecResult, error) {
	if len(argv) == 0 {
		return ExecResult{}, fmt.Errorf("sandbox docker exec: empty argv")
	}
	i.mu.Lock()
	container := i.container
	stopped := i.stopped
	killed := i.killed
	i.mu.Unlock()
	if stopped {
		return ExecResult{}, fmt.Errorf("sandbox docker exec: stopped")
	}
	if killed || container == "" {
		return ExecResult{}, fmt.Errorf("sandbox docker exec: killed")
	}
	args := append([]string{"exec", "-w", "/workspace", container}, argv...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return ExecResult{}, fmt.Errorf("sandbox docker exec: %w", err)
	}
	return res, nil
}

func (i *dockerInstance) ExportTar(ctx context.Context, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker export: %w", err)
	}
	i.mu.Lock()
	stopped := i.stopped
	merged := i.merged
	i.mu.Unlock()
	if stopped {
		return fmt.Errorf("sandbox docker export: stopped")
	}
	// Host-side tar of the overlay. Exec is docker exec in the
	// container, so this does not take a turn lock.
	if err := writeTar(ctx, merged, w); err != nil {
		return fmt.Errorf("sandbox docker export: %w", err)
	}
	return nil
}

func (i *dockerInstance) ImportTar(ctx context.Context, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	i.mu.Lock()
	stopped := i.stopped
	merged := i.merged
	i.mu.Unlock()
	if stopped {
		return fmt.Errorf("sandbox docker import: stopped")
	}
	if err := clearDir(merged); err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	if err := readTar(ctx, merged, r); err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	return nil
}

func (i *dockerInstance) Kill(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker kill: %w", err)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.stopped {
		return fmt.Errorf("sandbox docker kill: stopped")
	}
	if i.killed || i.container == "" {
		i.killed = true
		return nil
	}
	out, err := exec.CommandContext(ctx, "docker", "kill", i.container).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox docker kill: %s", bytesHead(out, err))
	}
	i.killed = true
	return nil
}

func (i *dockerInstance) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker stop: %w", err)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.stopped {
		return nil
	}
	i.stopped = true
	i.killed = true
	return i.cleanupLocked()
}

func (i *dockerInstance) cleanup() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.stopped = true
	i.killed = true
	return i.cleanupLocked()
}

func (i *dockerInstance) cleanupLocked() error {
	var first error
	if i.container != "" {
		out, err := exec.Command("docker", "rm", "-f", i.container).CombinedOutput()
		if err != nil && first == nil {
			first = fmt.Errorf("sandbox docker rm: %s", bytesHead(out, err))
		}
		i.container = ""
	}
	if i.proxy != nil {
		if err := i.proxy.Close(); err != nil && first == nil {
			first = fmt.Errorf("sandbox docker proxy: %w", err)
		}
		i.proxy = nil
	}
	if err := unmountOverlay(i.overlay, i.merged); err != nil && first == nil {
		first = fmt.Errorf("sandbox docker unmount: %w", err)
	}
	if i.base != "" {
		if err := forceRemoveAll(i.base); err != nil && first == nil {
			first = fmt.Errorf("sandbox docker cleanup: %w", err)
		}
	}
	return first
}

// forceRemoveAll deletes dir even when the container wrote files as
// root. CI runners cannot sudo; docker can still rm from inside.
func forceRemoveAll(dir string) error {
	if err := os.RemoveAll(dir); err == nil {
		return nil
	}
	if os.Getenv("SPIKE_SANDBOX_SUDO") == "1" {
		if _, err := exec.Command("sudo", "-n", "rm", "-rf", dir).CombinedOutput(); err == nil {
			return nil
		}
	}
	out, err := exec.Command("docker", "run", "--rm", "--network", "none",
		"--mount", "type=bind,src="+dir+",dst=/wipe",
		defaultImage, "sh", "-c", "find /wipe -mindepth 1 -delete").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", bytesHead(out, err))
	}
	return os.RemoveAll(dir)
}

// DockerReady reports whether the docker CLI can talk to a daemon.
func DockerReady() error {
	return dockerAvailable()
}

// dockerAvailable reports whether the docker CLI can talk to a daemon.
func dockerAvailable() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker info: %s", bytesHead(out, err))
	}
	return nil
}
