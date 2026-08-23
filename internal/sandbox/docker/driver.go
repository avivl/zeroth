package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/resilience"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/failsafe-go/failsafe-go"
)

const (
	driverName   = "docker"
	defaultImage = "alpine:3.20"
)

// Driver is a Docker sandbox. Workspaces live in a host directory
// bind-mounted at /workspace. Checkpoints are that tree packed as a
// tar, not a process snapshot.
type Driver struct {
	image  string
	root   string
	execer failsafe.Executor[struct{}]

	mu   sync.Mutex
	inst map[string]*instance
}

type instance struct {
	id        sandbox.ID
	workspace string
	container string

	mu      sync.Mutex
	killed  bool
	stopped bool
	bridged bool
	proxy   *egressProxy
}

// New returns a Docker sandbox driver.
func New() *Driver {
	opts := resilience.Defaults()
	opts.Timeout = 30 * time.Second
	breaker := resilience.NewBreaker[struct{}](opts)
	root := os.TempDir()
	if v := strings.TrimSpace(os.Getenv("ZEROTH_SANDBOX_ROOT")); v != "" {
		root = v
	}
	image := defaultImage
	if v := strings.TrimSpace(os.Getenv("ZEROTH_SANDBOX_IMAGE")); v != "" {
		image = v
	}
	return &Driver{
		image:  image,
		root:   root,
		execer: resilience.NewExecutor[struct{}](opts, breaker),
		inst:   make(map[string]*instance),
	}
}

// Name implements [sandbox.Driver].
func (*Driver) Name() string { return driverName }

var _ sandbox.Driver = (*Driver)(nil)

// Available reports whether the docker CLI can talk to a daemon.
func Available() error {
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

func (d *Driver) lookup(id sandbox.ID) (*instance, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("sandbox docker: %w", sandbox.ErrInvalid)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	inst, ok := d.inst[id.String()]
	if !ok {
		return nil, fmt.Errorf("sandbox docker: %w", sandbox.ErrNotFound)
	}
	return inst, nil
}

// HostWorkspace is the host directory bind-mounted at /workspace. The
// stage-1 harness is a host subprocess whose cwd is this tree
// (ADR-Z-0010), not a docker exec. Plan mode, no apply.
func (d *Driver) HostWorkspace(id sandbox.ID) (string, error) {
	inst, err := d.lookup(id)
	if err != nil {
		return "", fmt.Errorf("sandbox docker host workspace: %w", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.stopped {
		return "", fmt.Errorf("sandbox docker host workspace: %w", sandbox.ErrStopped)
	}
	if inst.workspace == "" {
		return "", fmt.Errorf("sandbox docker host workspace: %w", sandbox.ErrNotFound)
	}
	return inst.workspace, nil
}

func (d *Driver) docker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s", bytesHead(out, err))
	}
	return out, nil
}

func bytesHead(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if s == "" && err != nil {
		return err.Error()
	}
	if err != nil && s != "" {
		return err.Error() + ": " + s
	}
	return s
}

func currentUserSpec() string {
	uid := os.Getuid()
	gid := os.Getgid()
	if uid == 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", uid, gid)
}

func stdoutStderr(cmd *exec.Cmd) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
