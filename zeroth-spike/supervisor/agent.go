package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Agent emits tokens until ctx is cancelled or it exits.
type Agent interface {
	Name() string
	Run(ctx context.Context, emit func(payload string) error) error
}

// FakeAgent writes tok-N on a timer. It exists so G1/G6 do not need a
// model vendor. Interval is the gap after each token.
type FakeAgent struct {
	Interval time.Duration
}

// Name implements [Agent].
func (*FakeAgent) Name() string { return "fake" }

var _ Agent = (*FakeAgent)(nil)

// Run implements [Agent]. The first token is emitted immediately so a
// just-started session is attachable without waiting a full interval.
func (a *FakeAgent) Run(ctx context.Context, emit func(string) error) error {
	interval := a.Interval
	if interval <= 0 {
		interval = 20 * time.Millisecond
	}
	n := 0
	for {
		n++
		if err := emit(fmt.Sprintf("tok-%d", n)); err != nil {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// CmdAgent runs a subprocess and treats stdout chunks as tokens.
// Used for `claude -p` and for a stand-in command in tests.
type CmdAgent struct {
	Path string
	Args []string
}

// Name implements [Agent].
func (a *CmdAgent) Name() string {
	if a != nil && a.Path != "" {
		return "cmd:" + a.Path
	}
	return "cmd"
}

var _ Agent = (*CmdAgent)(nil)

// Run implements [Agent]. Stderr is discarded so a child that prints a
// key to stderr cannot put it in the event log.
func (a *CmdAgent) Run(ctx context.Context, emit func(string) error) error {
	if a.Path == "" {
		return fmt.Errorf("supervisor cmd: empty path")
	}
	cmd := exec.CommandContext(ctx, a.Path, a.Args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("supervisor cmd stdout: %w", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("supervisor cmd start: %w", err)
	}
	br := bufio.NewReader(stdout)
	buf := make([]byte, 256)
	for {
		n, err := br.Read(buf)
		if n > 0 {
			if emitErr := emit(string(buf[:n])); emitErr != nil {
				_ = cmd.Wait()
				return emitErr
			}
		}
		if err != nil {
			waitErr := cmd.Wait()
			if err == io.EOF {
				return waitErr
			}
			if waitErr != nil {
				return fmt.Errorf("supervisor cmd read: %v, wait: %w", err, waitErr)
			}
			return fmt.Errorf("supervisor cmd read: %w", err)
		}
	}
}

// ClaudePromptAgent is `claude -p` with a tiny prompt. Confirmation that
// the supervisor can run a real harness binary, not a product feature.
func ClaudePromptAgent() *CmdAgent {
	return &CmdAgent{
		Path: "claude",
		Args: []string{"-p", "Reply with the single word ok."},
	}
}
