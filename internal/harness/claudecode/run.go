package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/avivl/zeroth/internal/harness"
)

const stopWait = 2 * time.Second

// Start implements [harness.Driver].
func (d *Driver) Start(ctx context.Context, spec harness.Spec) (harness.Handle, error) {
	if err := ctx.Err(); err != nil {
		return harness.Handle{}, fmt.Errorf("harness claudecode start: %w", err)
	}
	if err := validateSpec(spec); err != nil {
		return harness.Handle{}, fmt.Errorf("harness claudecode start: %w", err)
	}
	if strings.TrimSpace(d.bin) == "" {
		return harness.Handle{}, fmt.Errorf("harness claudecode start: claude binary not found")
	}
	key := apiKeyFromEnvList(spec.Env)
	if key == "" {
		return harness.Handle{}, fmt.Errorf("harness claudecode start: %w", ErrMissingAPIKey)
	}

	id, err := harness.NewID()
	if err != nil {
		return harness.Handle{}, fmt.Errorf("harness claudecode start: %w", err)
	}
	inst := &instance{
		id:        id,
		key:       key,
		bin:       d.bin,
		wait:      make(chan struct{}),
		workspace: spec.Workspace,
		prompt:    spec.Prompt,
		env:       append([]string(nil), spec.Env...),
		session:   spec.Resume.VendorSession,
	}
	if spec.Resume.Transcript != "" {
		inst.transcript.WriteString(spec.Resume.Transcript)
	}
	if err := inst.spawn(spec.Prompt, spec.Resume.VendorSession); err != nil {
		return harness.Handle{}, fmt.Errorf("harness claudecode start: %w", err)
	}

	d.mu.Lock()
	d.inst[id.String()] = inst
	d.mu.Unlock()
	return harness.Handle{ID: id}, nil
}

func validateSpec(spec harness.Spec) error {
	if strings.TrimSpace(spec.Prompt) == "" {
		return harness.ErrInvalid
	}
	if strings.TrimSpace(spec.Workspace) == "" {
		return harness.ErrInvalid
	}
	info, err := os.Stat(spec.Workspace)
	if err != nil || !info.IsDir() {
		return harness.ErrInvalid
	}
	for _, e := range spec.Env {
		if !strings.Contains(e, "=") {
			return harness.ErrInvalid
		}
	}
	return nil
}

func cliArgs(prompt, resumeSession string) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--bare",
		"--tools", "",
		"--permission-mode", "plan",
		"--input-format", "stream-json",
		"--system-prompt", ProposeEffectsPrompt,
	}
	if resumeSession != "" {
		args = append(args, "--resume", resumeSession)
	}
	args = append(args, prompt)
	return args
}

func (i *instance) spawn(prompt, resumeSession string) error {
	cmd := exec.Command(i.bin, cliArgs(prompt, resumeSession)...)
	cmd.Dir = i.workspace
	cmd.Env = childEnv(i.env, i.key)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("exec: %w", err)
	}

	done := make(chan struct{})
	i.mu.Lock()
	i.cmd = cmd
	i.stdin = stdin
	i.exited = false
	i.done = done
	i.mu.Unlock()

	go func() {
		defer close(done)
		i.read(stdout, &stderr)
	}()
	return nil
}

func childEnv(specEnv []string, key string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+len(specEnv)+1)
	for _, e := range env {
		name, _, ok := strings.Cut(e, "=")
		if ok && name == apiKeyEnv {
			continue
		}
		out = append(out, e)
	}
	seen := map[string]int{}
	for idx, e := range out {
		name, _, ok := strings.Cut(e, "=")
		if ok {
			seen[name] = idx
		}
	}
	put := func(e string) {
		name, _, ok := strings.Cut(e, "=")
		if !ok {
			return
		}
		if idx, exists := seen[name]; exists {
			out[idx] = e
			return
		}
		seen[name] = len(out)
		out = append(out, e)
	}
	if key != "" {
		put(apiKeyEnv + "=" + key)
	}
	for _, e := range specEnv {
		put(e)
	}
	return out
}

func (i *instance) read(stdout io.Reader, stderr *bytes.Buffer) {
	ex := &extractor{key: i.key}
	i.mu.Lock()
	ex.session = i.session
	i.mu.Unlock()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var transcript strings.Builder
	for sc.Scan() {
		line := sc.Text()
		transcript.WriteString(line)
		transcript.WriteByte('\n')
		for _, ev := range ex.handleLine(line) {
			i.emit(ev)
		}
	}
	_ = sc.Err()

	i.mu.Lock()
	cmd := i.cmd
	i.mu.Unlock()
	waitErr := error(nil)
	if cmd != nil {
		waitErr = cmd.Wait()
	}

	i.mu.Lock()
	i.transcript.WriteString(transcript.String())
	if ex.session != "" {
		i.session = ex.session
	}
	stopping := i.stopping
	full := i.transcript.String()
	i.exited = true
	i.cmd = nil
	i.stdin = nil
	i.mu.Unlock()

	if !i.hasKind(harness.EventEffects) {
		if effects, err := harness.ParseEffects(full); err == nil {
			i.emit(harness.Event{Kind: harness.EventEffects, Effects: effects})
		}
	}

	payload := exitPayload(waitErr, stopping, stderr, i.key)
	i.emit(harness.Event{Kind: harness.EventExited, Payload: payload})
}

func (i *instance) hasKind(k harness.EventKind) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, ev := range i.events {
		if ev.Kind == k {
			return true
		}
	}
	return false
}

func exitPayload(waitErr error, stopping bool, stderr *bytes.Buffer, key string) string {
	if stopping {
		return "stopped"
	}
	if waitErr == nil {
		return "exit 0"
	}
	if ee, ok := waitErr.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return "killed"
		}
		return fmt.Sprintf("exit %d", ee.ExitCode())
	}
	msg := waitErr.Error()
	if stderr != nil {
		if extra := strings.TrimSpace(stderr.String()); extra != "" {
			msg = msg + ": " + extra
		}
	}
	return redact(msg, key)
}

// Stream implements [harness.Driver].
func (d *Driver) Stream(ctx context.Context, id harness.ID) (<-chan harness.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("harness claudecode stream: %w", err)
	}
	inst, err := d.lookup(id)
	if err != nil {
		return nil, fmt.Errorf("harness claudecode stream: %w", err)
	}
	ch := make(chan harness.Event, 32)
	go func() {
		defer close(ch)
		var seq int
		for {
			events, wait, closed := inst.snapshot()
			for seq < len(events) {
				ev := events[seq]
				seq++
				select {
				case <-ctx.Done():
					return
				case ch <- ev:
				}
			}
			if closed {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-wait:
			}
		}
	}()
	return ch, nil
}

// Steer implements [harness.Driver].
func (d *Driver) Steer(ctx context.Context, id harness.ID, msg string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("harness claudecode steer: %w", err)
	}
	if strings.TrimSpace(msg) == "" {
		return fmt.Errorf("harness claudecode steer: %w", harness.ErrInvalid)
	}
	inst, err := d.lookup(id)
	if err != nil {
		return fmt.Errorf("harness claudecode steer: %w", err)
	}
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return fmt.Errorf("harness claudecode steer: %w", harness.ErrStopped)
	}
	stdin := inst.stdin
	exited := inst.exited
	inst.mu.Unlock()

	if !exited && stdin != nil {
		if err := writeUserMessage(stdin, msg); err != nil {
			return fmt.Errorf("harness claudecode steer: %w", err)
		}
		return nil
	}
	if err := inst.restart(msg); err != nil {
		return fmt.Errorf("harness claudecode steer: %w", err)
	}
	return nil
}

func (i *instance) restart(msg string) error {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return harness.ErrStopped
	}
	if !i.exited {
		i.mu.Unlock()
		return nil
	}
	resume := i.session
	prompt := msg
	if resume == "" {
		prev := strings.TrimSpace(i.transcript.String())
		if prev != "" {
			prompt = prev + "\n\n" + msg
		}
	}
	i.mu.Unlock()
	return i.spawn(prompt, resume)
}

func writeUserMessage(w io.Writer, text string) error {
	type part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type body struct {
		Role    string `json:"role"`
		Content []part `json:"content"`
	}
	type msg struct {
		Type    string `json:"type"`
		Message body   `json:"message"`
	}
	raw, err := json.Marshal(msg{
		Type:    "user",
		Message: body{Role: "user", Content: []part{{Type: "text", Text: text}}},
	})
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = w.Write(raw)
	return err
}

// Checkpoint implements [harness.Driver].
func (d *Driver) Checkpoint(ctx context.Context, id harness.ID) (harness.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return harness.Checkpoint{}, fmt.Errorf("harness claudecode checkpoint: %w", err)
	}
	inst, err := d.lookup(id)
	if err != nil {
		return harness.Checkpoint{}, fmt.Errorf("harness claudecode checkpoint: %w", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return harness.Checkpoint{}, fmt.Errorf("harness claudecode checkpoint: %w", harness.ErrStopped)
	}
	return harness.Checkpoint{
		VendorSession: inst.session,
		Transcript:    redact(inst.transcript.String(), inst.key),
	}, nil
}

// Stop implements [harness.Driver].
func (d *Driver) Stop(ctx context.Context, id harness.ID) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("harness claudecode stop: %w", err)
	}
	d.mu.Lock()
	inst, ok := d.inst[id.String()]
	if !ok {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	inst.kill()

	d.mu.Lock()
	delete(d.inst, id.String())
	d.mu.Unlock()
	return nil
}

func (i *instance) kill() {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return
	}
	i.stopping = true
	pid := 0
	if i.cmd != nil && i.cmd.Process != nil {
		pid = i.cmd.Process.Pid
	}
	stdin := i.stdin
	done := i.done
	exited := i.exited
	i.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if pid > 0 && !exited {
		signalGroup(pid, syscall.SIGTERM)
		timer := time.NewTimer(stopWait)
		if done == nil {
			timer.Stop()
		} else {
			select {
			case <-done:
				timer.Stop()
			case <-timer.C:
				signalGroup(pid, syscall.SIGKILL)
				<-done
			}
		}
	} else if done != nil && !exited {
		select {
		case <-done:
		case <-time.After(stopWait):
		}
	}

	i.mu.Lock()
	i.closed = true
	select {
	case <-i.wait:
	default:
		close(i.wait)
	}
	i.wait = make(chan struct{})
	i.mu.Unlock()
}

func signalGroup(pid int, sig syscall.Signal) {
	if err := syscall.Kill(-pid, sig); err != nil {
		_ = syscall.Kill(pid, sig)
	}
}
