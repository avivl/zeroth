package memory

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/sandbox/docker"
)

func TestHydrateSandboxWritesAndExportOmitsCompiled(t *testing.T) {
	if err := docker.Available(); err != nil {
		t.Skipf("docker sandbox unavailable: %v", err)
	}
	d := docker.New()
	ctx := t.Context()
	sb, err := d.Spawn(ctx, sandbox.Spec{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(ctx, sb.ID) })
	if _, err := d.Exec(ctx, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo kept > /workspace/kept.txt"}}); err != nil {
		t.Fatal(err)
	}

	b := NewBook()
	if _, err := b.Write(Human("alice"), KindOperator, "", "style.tests", "prefer table tests", "operator"); err != nil {
		t.Fatal(err)
	}
	if err := HydrateSandbox(ctx, d, sb.ID, b.Slice()); err != nil {
		t.Fatal(err)
	}
	res, err := d.Exec(ctx, sb.ID, sandbox.Cmd{Argv: []string{"cat", "/workspace/AGENTS.md"}})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("cat AGENTS.md: %+v err=%v", res, err)
	}
	if res.Stdout != Compile(b.Slice()) {
		t.Fatalf("sandbox AGENTS.md does not match compile\n%s", res.Stdout)
	}

	var buf bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &buf); err != nil {
		t.Fatal(err)
	}
	names := tarNames(t, buf.Bytes())
	if !names["kept.txt"] {
		t.Fatalf("export missing kept.txt: %v", names)
	}
	for _, forbidden := range CompiledPaths() {
		if names[forbidden] {
			t.Fatalf("checkpoint contains compiled %q: %v", forbidden, names)
		}
	}
}

func TestProtectSandboxScriptDoesNotSwallowFailures(t *testing.T) {
	t.Parallel()
	script := protectSandboxScript()
	if !strings.Contains(script, "set -e") {
		t.Fatal("protect script must fail closed (set -e)")
	}
	if strings.Contains(script, "|| true") {
		t.Fatal("protect script still swallows skip-worktree/exclude failures with || true")
	}
	for _, p := range CompiledPaths() {
		if !strings.Contains(script, p) {
			t.Fatalf("protect script missing compiled path %q", p)
		}
	}
}

func TestHydrateSandboxProtectExitIsSurfaced(t *testing.T) {
	t.Parallel()
	d := &stubExecDriver{onExec: func(cmd sandbox.Cmd) (sandbox.ExecResult, error) {
		if isProtectCmd(cmd) {
			return sandbox.ExecResult{ExitCode: 1, Stderr: "git update-index: index is locked"}, nil
		}
		return sandbox.ExecResult{ExitCode: 0}, nil
	}}
	err := HydrateSandbox(t.Context(), d, sandbox.ID{}, nil)
	if err == nil {
		t.Fatal("hydrate sandbox succeeded despite protect exit 1")
	}
	got := err.Error()
	if !strings.Contains(got, "protect") {
		t.Fatalf("error %q does not name protect", got)
	}
	if !strings.Contains(got, "index is locked") {
		t.Fatalf("error %q does not include protect stderr", got)
	}
}

func TestHydrateSandboxProtectDriverErrorIsSurfaced(t *testing.T) {
	t.Parallel()
	want := errors.New("sandbox exec: killed")
	d := &stubExecDriver{onExec: func(cmd sandbox.Cmd) (sandbox.ExecResult, error) {
		if isProtectCmd(cmd) {
			return sandbox.ExecResult{}, want
		}
		return sandbox.ExecResult{ExitCode: 0}, nil
	}}
	err := HydrateSandbox(t.Context(), d, sandbox.ID{}, nil)
	if err == nil {
		t.Fatal("hydrate sandbox succeeded despite protect driver error")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error %v is not the driver error", err)
	}
	if !strings.Contains(err.Error(), "protect") {
		t.Fatalf("error %q does not name protect", err)
	}
}

func isProtectCmd(cmd sandbox.Cmd) bool {
	for _, arg := range cmd.Argv {
		if strings.Contains(arg, "skip-worktree") || strings.Contains(arg, "info/exclude") {
			return true
		}
	}
	return false
}

type stubExecDriver struct {
	onExec func(sandbox.Cmd) (sandbox.ExecResult, error)
}

func (d *stubExecDriver) Name() string { return "stub-exec" }

func (d *stubExecDriver) Spawn(context.Context, sandbox.Spec) (sandbox.Sandbox, error) {
	return sandbox.Sandbox{}, nil
}

func (d *stubExecDriver) Exec(_ context.Context, _ sandbox.ID, cmd sandbox.Cmd) (sandbox.ExecResult, error) {
	if d.onExec != nil {
		return d.onExec(cmd)
	}
	return sandbox.ExecResult{}, nil
}

func (d *stubExecDriver) ExportTar(context.Context, sandbox.ID, io.Writer) error { return nil }
func (d *stubExecDriver) ImportTar(context.Context, sandbox.ID, io.Reader) error { return nil }
func (d *stubExecDriver) AllowEgress(context.Context, sandbox.ID, []sandbox.EgressRule) error {
	return nil
}
func (d *stubExecDriver) Kill(context.Context, sandbox.ID) error { return nil }
func (d *stubExecDriver) Stop(context.Context, sandbox.ID) error { return nil }

var _ sandbox.Driver = (*stubExecDriver)(nil)

func tarNames(t *testing.T, raw []byte) map[string]bool {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(raw))
	out := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimPrefix(hdr.Name, "./")] = true
	}
}
