package sandbox_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/avivl/zeroth/zeroth-spike/sandbox"
	"github.com/avivl/zeroth/zeroth-spike/session"
)

func TestDriverNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		driver sandbox.Driver
	}{
		{name: "memory", driver: sandbox.NewMemory()},
		{name: "docker", driver: sandbox.NewDocker()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.driver.Name(); got != tc.name {
				t.Fatalf("Name() = %q, want %q", got, tc.name)
			}
		})
	}
}

func TestDockerStartIsStub(t *testing.T) {
	t.Parallel()
	id, err := session.ParseID("sess-docker")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sandbox.NewDocker().Start(context.Background(), sandbox.StartRequest{
		SessionID: id,
		Workspace: sandbox.Workspace{TarPath: "unused.tar"},
	})
	if err == nil {
		t.Fatal("expected setup stub error")
	}
}

func TestMemoryStartExecStop(t *testing.T) {
	t.Parallel()

	tarPath := writeHelloTar(t)
	id, err := session.ParseID("sess-mem")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := sandbox.NewMemory().Start(context.Background(), sandbox.StartRequest{
		SessionID: id,
		Workspace: sandbox.Workspace{TarPath: tarPath},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		_ = inst.Stop(context.Background())
	})
	if inst.SessionID() != id {
		t.Fatalf("session id mismatch")
	}
	if inst.ID().IsZero() {
		t.Fatal("zero handle id")
	}

	res, err := inst.Exec(context.Background(), []string{"cat", "hello.txt"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, stdout = %q", res.ExitCode, res.Stdout)
	}
	if res.Stdout != "hello spike\n" {
		t.Fatalf("stdout = %q", res.Stdout)
	}

	if err := inst.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := inst.Exec(context.Background(), []string{"cat", "hello.txt"}); err == nil {
		t.Fatal("exec after stop: expected error")
	}
}

func TestMemoryStartRejectsEmptySession(t *testing.T) {
	t.Parallel()
	_, err := sandbox.NewMemory().Start(context.Background(), sandbox.StartRequest{
		Workspace: sandbox.Workspace{TarPath: "x.tar"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMemoryRejectsEscapingTar(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, "nope"); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "evil.tar")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := session.ParseID("sess-evil")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sandbox.NewMemory().Start(context.Background(), sandbox.StartRequest{
		SessionID: id,
		Workspace: sandbox.Workspace{TarPath: path},
	})
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func writeHelloTar(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := "hello spike\n"
	hdr := &tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hello.tar")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
