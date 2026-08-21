package claudecode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/resilience"
	"github.com/failsafe-go/failsafe-go"
)

const driverName = "claudecode"

// Driver is a Claude Code harness. It supervises a `claude` subprocess
// and speaks the [harness.Driver] protocol back to core.
type Driver struct {
	bin    string
	execer failsafe.Executor[struct{}]

	mu   sync.Mutex
	inst map[string]*instance
}

type instance struct {
	id  harness.ID
	key string
	bin string

	mu         sync.Mutex
	cmd        *exec.Cmd
	stdin      stdinWriter
	events     []harness.Event
	wait       chan struct{}
	done       chan struct{} // closed when the current reader returns
	closed     bool          // Stop completed
	exited     bool          // current subprocess has exited
	stopping   bool
	session    string
	transcript strings.Builder
	workspace  string
	prompt     string
	env        []string
}

type stdinWriter interface {
	Write([]byte) (int, error)
	Close() error
}

// New returns a Claude Code harness driver. The binary is CLAUDE_BIN,
// then `claude` on PATH, then ~/.local/bin/claude.
func New() *Driver {
	return NewWithBin(lookupClaudeBin())
}

// NewWithBin returns a driver that runs bin as the Claude Code CLI.
// Tests inject a fake. Empty bin is allowed for Name(); Start fails.
func NewWithBin(bin string) *Driver {
	opts := resilience.Defaults()
	opts.Timeout = 10 * time.Second
	breaker := resilience.NewBreaker[struct{}](opts)
	return &Driver{
		bin:    bin,
		execer: resilience.NewExecutor[struct{}](opts, breaker),
		inst:   make(map[string]*instance),
	}
}

// Name implements [harness.Driver].
func (*Driver) Name() string { return driverName }

var _ harness.Driver = (*Driver)(nil)

func lookupClaudeBin() string {
	if p := strings.TrimSpace(os.Getenv("CLAUDE_BIN")); p != "" {
		return p
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(home, ".local", "bin", "claude")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func (d *Driver) lookup(id harness.ID) (*instance, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("harness claudecode: %w", harness.ErrInvalid)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	inst, ok := d.inst[id.String()]
	if !ok {
		return nil, fmt.Errorf("harness claudecode: %w", harness.ErrNotFound)
	}
	return inst, nil
}

func (i *instance) emit(ev harness.Event) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return
	}
	i.events = append(i.events, ev)
	close(i.wait)
	i.wait = make(chan struct{})
}

func (i *instance) snapshot() (events []harness.Event, wait <-chan struct{}, closed bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	events = append([]harness.Event(nil), i.events...)
	return events, i.wait, i.closed
}
