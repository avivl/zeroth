package memory

import (
	"archive/tar"
	"bytes"
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
