package claudecode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/harness"
)

const testKey = "sk-ant-zeroth-driver-test-not-a-real-key"

func TestStartRequiresAPIKey(t *testing.T) {
	t.Setenv(apiKeyEnv, "")
	d := NewWithBin(buildFake(t))
	_, err := d.Start(t.Context(), harness.Spec{
		Workspace: t.TempDir(),
		Prompt:    "hi",
	})
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("err = %v, want ErrMissingAPIKey", err)
	}
}

func TestStartRequiresBinary(t *testing.T) {
	t.Parallel()
	d := NewWithBin("")
	_, err := d.Start(t.Context(), harness.Spec{
		Workspace: t.TempDir(),
		Prompt:    "hi",
		Env:       []string{apiKeyEnv + "=" + testKey},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExternalKillSurfacesExited(t *testing.T) {
	t.Parallel()
	d := NewWithBin(buildFake(t))
	ws := t.TempDir()
	h, err := d.Start(t.Context(), harness.Spec{
		Workspace: ws,
		Prompt:    "SLEEP please",
		Env: []string{
			apiKeyEnv + "=" + testKey,
			"ZEROTH_FAKE_PID=1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })

	ch, err := d.Stream(t.Context(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitToken(t, ch, "sleep-token", 10*time.Second)

	pid := readPID(t, filepath.Join(ws, ".fake-pid"))
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill %d: %v", pid, err)
	}

	deadline := time.After(8 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("stream closed without exited event")
			}
			if ev.Kind == harness.EventExited {
				if ev.Payload == "" {
					t.Fatal("empty exited payload")
				}
				return
			}
		case <-deadline:
			t.Fatal("external kill was not surfaced as an exited event")
		}
	}
}

func TestSpawnIsHostSubprocess(t *testing.T) {
	t.Parallel()
	// ADR-Z-0010: claude is a host os/exec child, not sandbox.Exec.
	// A later in-sandbox spawn must supersede that ADR and invert this
	// test: the host canary would stay untouched, and /proc/<pid> would
	// not be this process's pid namespace.
	d := NewWithBin(buildFake(t))
	ws := t.TempDir()
	outside := t.TempDir()
	canary := filepath.Join(outside, "host-canary")
	h, err := d.Start(t.Context(), harness.Spec{
		Workspace: ws,
		Prompt:    "SLEEP please",
		Env: []string{
			apiKeyEnv + "=" + testKey,
			"ZEROTH_FAKE_PID=1",
			"ZEROTH_FAKE_HOST_WRITE=" + canary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })

	ch, err := d.Stream(t.Context(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitToken(t, ch, "sleep-token", 10*time.Second)

	pid := readPID(t, filepath.Join(ws, ".fake-pid"))
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("harness pid %d is not a live host process: %v", pid, err)
	}
	cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		t.Fatalf("read cwd of pid %d: %v", pid, err)
	}
	want, err := filepath.EvalSymlinks(ws)
	if err != nil {
		want = ws
	}
	got, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		got = cwd
	}
	if got != want {
		t.Fatalf("harness cwd %q, want overlay %q", got, want)
	}

	deadline := time.Now().Add(2 * time.Second)
	var body []byte
	for time.Now().Before(deadline) {
		body, err = os.ReadFile(canary)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("host canary outside workspace was not written: %v", err)
	}
	if string(body) != "host-subprocess" {
		t.Fatalf("host canary = %q, want host-subprocess", body)
	}
}

func TestSteerAfterExitRestarts(t *testing.T) {
	t.Parallel()
	d := NewWithBin(buildFake(t))
	h, err := d.Start(t.Context(), harness.Spec{
		Workspace: t.TempDir(),
		Prompt:    "one shot",
		Env:       []string{apiKeyEnv + "=" + testKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })
	ch, err := d.Stream(t.Context(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitKind(t, ch, harness.EventExited, 10*time.Second)
	if err := d.Steer(t.Context(), h.ID, "HOLD after exit"); err != nil {
		t.Fatalf("Steer restart: %v", err)
	}
	waitToken(t, ch, "hello-token", 10*time.Second)
}

func TestStartIgnoresBrokenPipeWhenChildExits(t *testing.T) {
	t.Parallel()
	d := NewWithBin(buildFake(t))
	h, err := d.Start(t.Context(), harness.Spec{
		Workspace: t.TempDir(),
		Prompt:    "stop twice",
		Env: []string{
			apiKeyEnv + "=" + testKey,
			"ZEROTH_FAKE_EXIT_BEFORE_STDIN=1",
		},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatalf("Stop again: %v", err)
	}
}

func TestWriteUserMessageBrokenPipe(t *testing.T) {
	t.Parallel()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	err = writeUserMessage(w, "hi")
	if !isBrokenPipe(err) {
		t.Fatalf("err = %v, want broken pipe", err)
	}
}

func TestStartWritesPromptOnStdin(t *testing.T) {
	t.Parallel()
	d := NewWithBin(buildFake(t))
	h, err := d.Start(t.Context(), harness.Spec{
		Workspace: t.TempDir(),
		Prompt:    "STDINPROMPT please",
		Env:       []string{apiKeyEnv + "=" + testKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })
	ch, err := d.Stream(t.Context(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitToken(t, ch, "from-stdin", 10*time.Second)
}

func TestStopAfterStopIsNil(t *testing.T) {
	t.Parallel()
	d := NewWithBin(buildFake(t))
	id, err := harness.ParseID("hrn_never")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(t.Context(), id); err != nil {
		t.Fatalf("Stop unknown: %v", err)
	}
}

func waitToken(t *testing.T, ch <-chan harness.Event, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed looking for %q", needle)
			}
			if ev.Kind == harness.EventToken && strings.Contains(ev.Payload, needle) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout looking for %q", needle)
		}
	}
}

// waitKind drains ch until an event of kind k arrives and returns everything
// it saw, k included, so callers can assert on the run as a whole.
func waitKind(t *testing.T, ch <-chan harness.Event, k harness.EventKind, timeout time.Duration) []harness.Event {
	t.Helper()
	var seen []harness.Event
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed looking for %s", k)
			}
			seen = append(seen, ev)
			if ev.Kind == k {
				return seen
			}
		case <-deadline:
			t.Fatalf("timeout looking for %s after %d events", k, len(seen))
		}
	}
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
			if err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid file %s not written", path)
	return 0
}

func buildFake(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join("testdata", "fakecli")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fakecli: %v\n%s", err, out)
	}
	return bin
}

func TestTruncatedStreamIsNotACleanExit(t *testing.T) {
	t.Parallel()
	// 42-67: the scan can end in an error rather than EOF. A child that
	// then exits 0 must not be reported as a clean exit, and the partial
	// transcript must not be salvaged into a proposed plan.
	d := NewWithBin(buildFake(t))
	h, err := d.Start(t.Context(), harness.Spec{
		Workspace: t.TempDir(),
		Prompt:    "TRUNCATE please",
		Env:       []string{apiKeyEnv + "=" + testKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })

	ch, err := d.Stream(t.Context(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	events := waitKind(t, ch, harness.EventExited, 20*time.Second)

	var exited, errored *harness.Event
	for i := range events {
		ev := &events[i]
		if strings.Contains(ev.Payload, testKey) {
			t.Fatalf("api key leaked into %s payload", ev.Kind)
		}
		switch ev.Kind {
		case harness.EventExited:
			exited = ev
		case harness.EventError:
			errored = ev
		}
	}

	if exited == nil {
		t.Fatal("no exited event")
	}
	// The child exited 0. The payload has to carry both facts: the stream
	// broke, and the process itself ended cleanly.
	if !strings.HasPrefix(exited.Payload, "stream error: ") || !strings.HasSuffix(exited.Payload, "; exit 0") {
		t.Fatalf("exited payload = %q, want \"stream error: ...; exit 0\"", exited.Payload)
	}
	if errored == nil {
		t.Fatal("no error event for the failed scan")
	}
	if !strings.Contains(errored.Payload, "harness claudecode stdout") {
		t.Fatalf("error payload = %q, want the wrapped scan error", errored.Payload)
	}
}

func TestTruncatedStreamDoesNotSalvageEffects(t *testing.T) {
	t.Parallel()
	// 42-67: with no effects event from the scan, read() falls back to
	// ParseEffects over the whole transcript, which on a resume already
	// holds the prior turn's effects. Salvaging those would let a truncated
	// turn propose a plan it never actually produced. TRUNCATEFIRST breaks
	// the stream before any line reaches the transcript, so the resumed
	// effects are the only thing left to parse.
	d := NewWithBin(buildFake(t))
	h, err := d.Start(t.Context(), harness.Spec{
		Workspace: t.TempDir(),
		Prompt:    "TRUNCATEFIRST please",
		Env:       []string{apiKeyEnv + "=" + testKey},
		Resume: harness.Checkpoint{
			Transcript: `{"effects":[{"op":"modify","target":"README.md","diff":"+prior turn"}]}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })

	ch, err := d.Stream(t.Context(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range waitKind(t, ch, harness.EventExited, 20*time.Second) {
		if ev.Kind == harness.EventEffects {
			t.Fatalf("truncated turn salvaged effects from the resumed transcript: %#v", ev.Effects)
		}
	}
}

func TestExitPayload(t *testing.T) {
	t.Parallel()
	scanErr := errors.New("boom")
	cases := []struct {
		name     string
		scanErr  error
		stopping bool
		want     string
	}{
		// 42-67: Stop is set after the scan already failed, so it must not
		// rewrite the outcome into a plain "stopped".
		{"truncated then stopped", scanErr, true, "stream error: boom; stopped"},
		{"truncated clean exit", scanErr, false, "stream error: boom; exit 0"},
		{"stopped", nil, true, "stopped"},
		{"clean exit", nil, false, "exit 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := exitPayload(nil, tc.scanErr, tc.stopping, nil, testKey); got != tc.want {
				t.Fatalf("payload = %q, want %q", got, tc.want)
			}
		})
	}
}
