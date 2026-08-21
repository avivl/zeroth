package claudecode

import (
	"context"
	"errors"
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

func waitKind(t *testing.T, ch <-chan harness.Event, k harness.EventKind, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed looking for %s", k)
			}
			if ev.Kind == k {
				return
			}
		case <-deadline:
			t.Fatalf("timeout looking for %s", k)
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
