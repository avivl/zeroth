package sandbox_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/sandbox"
	"github.com/avivl/zeroth/zeroth-spike/session"
)

func requireDocker(t *testing.T) *sandbox.Docker {
	t.Helper()
	if err := sandbox.DockerReady(); err != nil {
		t.Skipf("docker sandbox unavailable: %v", err)
	}
	return sandbox.NewDocker()
}

func TestDockerStartExecStopIsolation(t *testing.T) {
	d := requireDocker(t)
	tarPath := writeHelloTar(t)
	id, err := session.ParseID("sess-docker")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	inst, err := d.Start(ctx, sandbox.StartRequest{
		SessionID: id,
		Workspace: sandbox.Workspace{TarPath: tarPath},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = inst.Stop(ctx) })

	res, err := inst.Exec(ctx, []string{"cat", "hello.txt"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d stderr=%q stdout=%q", res.ExitCode, res.Stderr, res.Stdout)
	}
	if res.Stdout != "hello spike\n" {
		t.Fatalf("stdout = %q", res.Stdout)
	}

	canary := filepath.Join(t.TempDir(), "host-canary")
	if err := os.WriteFile(canary, []byte("host"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = inst.Exec(ctx, []string{"sh", "-c", "echo sandbox > '" + canary + "' || true"})
	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "host" {
		t.Fatalf("sandbox wrote through to host canary: %q", got)
	}

	if err := inst.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := inst.Exec(ctx, []string{"cat", "hello.txt"}); err == nil {
		t.Fatal("exec after stop: expected error")
	}
}

func TestDockerRoundTripHash(t *testing.T) {
	d := requireDocker(t)
	tarPath := writeHelloTar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	got, err := sandbox.RoundTrip(ctx, d, tarPath)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Match {
		t.Fatal("hash mismatch")
	}
	if got.Hash == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("workspace hash is empty; overlay did not show imported files")
	}
	t.Logf("overlay=%s ingest=%s export=%s restore=%s hash=%s", got.Overlay, got.Ingest, got.Export, got.Restore, got.Hash)
}

func TestDockerKillResume(t *testing.T) {
	d := requireDocker(t)
	tarPath := writeHelloTar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	got, err := sandbox.KillResume(ctx, d, tarPath, 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ResumeClean {
		t.Fatalf("resume not clean: %+v", got)
	}
	if got.LostFiles < 1 {
		t.Fatalf("expected lost work since last export, got %+v", got)
	}
	t.Logf("checkpoint=%d restored=%d lost=%d overlay=%s", got.CheckpointFiles, got.RestoredFiles, got.LostFiles, got.Overlay)
}

func TestDockerDaemonRestore(t *testing.T) {
	d := requireDocker(t)
	tarPath := writeHelloTar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	got, err := sandbox.DaemonRestore(ctx, d, tarPath)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LimitationHolds {
		t.Fatalf("daemon limitation did not hold: %+v", got)
	}
}

func TestDockerAsyncExport(t *testing.T) {
	d := requireDocker(t)
	tarPath := writeHelloTar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	got, err := sandbox.AsyncExport(ctx, d, tarPath, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocked {
		t.Fatalf("export blocked turn: %+v", got)
	}
}

func TestDockerSFixtureRoundTrip(t *testing.T) {
	sTar := filepath.Join(fixturesDir(t), "S.tar")
	if _, err := os.Stat(sTar); err != nil {
		t.Skip("S.tar not present")
	}
	d := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	got, err := sandbox.RoundTrip(ctx, d, sTar)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Match {
		t.Fatal("S.tar round-trip hash mismatch")
	}
	if got.Hash == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("S.tar workspace hash is empty")
	}
	t.Logf("S overlay=%s ingest=%s export=%s restore=%s", got.Overlay, got.Ingest, got.Export, got.Restore)
}

func fixturesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "fixtures")
}
