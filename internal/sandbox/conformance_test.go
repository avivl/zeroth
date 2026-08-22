package sandbox_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/sandbox/docker"
)

type harness struct {
	name string
	open func(t *testing.T) sandbox.Driver
}

func TestDriverConformance(t *testing.T) {
	t.Parallel()

	// Adding a backend is one more row. The cases below must stay
	// implementation-agnostic (NFR-4, Z1-080).
	cases := []harness{
		{
			name: "docker",
			open: func(t *testing.T) sandbox.Driver {
				t.Helper()
				if err := docker.Available(); err != nil {
					t.Skipf("docker sandbox unavailable: %v", err)
				}
				return docker.New()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := tc.open(t)
			if d == nil {
				t.Fatal("nil driver")
			}
			if got := d.Name(); got != tc.name {
				t.Fatalf("Name() = %q, want %q", got, tc.name)
			}

			t.Run("spawn_exec_stop", func(t *testing.T) { testSpawnExecStop(t, tc.open) })
			t.Run("exit_codes", func(t *testing.T) { testExitCodes(t, tc.open) })
			t.Run("env_visibility", func(t *testing.T) { testEnvVisibility(t, tc.open) })
			t.Run("host_isolation", func(t *testing.T) { testHostIsolation(t, tc.open) })
			t.Run("overlay_workspace", func(t *testing.T) { testOverlayWorkspace(t, tc.open) })
			t.Run("tar_round_trip_hash", func(t *testing.T) { testTarRoundTripHash(t, tc.open) })
			t.Run("import_rejects_escape", func(t *testing.T) { testImportRejectsEscape(t, tc.open) })
			t.Run("egress_deny_default", func(t *testing.T) { testEgressDenyDefault(t, tc.open) })
			t.Run("egress_allow_listed", func(t *testing.T) { testEgressAllowListed(t, tc.open) })
			t.Run("kill_inflight", func(t *testing.T) { testKillInflight(t, tc.open) })
			t.Run("kill_lost_work", func(t *testing.T) { testKillLostWork(t, tc.open) })
			t.Run("kill_daemon_not_restored", func(t *testing.T) { testKillDaemonNotRestored(t, tc.open) })
			t.Run("export_alongside_exec", func(t *testing.T) { testExportAlongsideExec(t, tc.open) })
			t.Run("cred_files_tmpfs", func(t *testing.T) { testCredFilesTmpfs(t, tc.open) })
			t.Run("credential_excluded_from_export", func(t *testing.T) { testCredentialExcludedFromExport(t, tc.open) })
			t.Run("compiled_memory_excluded_from_export", func(t *testing.T) { testCompiledMemoryExcludedFromExport(t, tc.open) })
			t.Run("export_secret_fail_closed", func(t *testing.T) { testExportSecretFailClosed(t, tc.open) })
			t.Run("branch_independent", func(t *testing.T) { testBranchIndependent(t, tc.open) })
			t.Run("unknown_id", func(t *testing.T) { testUnknownID(t, tc.open) })
			t.Run("stop_idempotent", func(t *testing.T) { testStopIdempotent(t, tc.open) })
		})
	}
}

func testSpawnExecStop(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())
	res := mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"cat", "hello.txt"}})
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d stderr=%q stdout=%q", res.ExitCode, res.Stderr, res.Stdout)
	}
	if res.Stdout != "hello sandbox\n" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
	if err := d.Stop(ctx, sb.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := d.Exec(ctx, sb.ID, sandbox.Cmd{Argv: []string{"cat", "hello.txt"}}); !errors.Is(err, sandbox.ErrStopped) && !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("exec after stop: %v", err)
	}
}

func testExitCodes(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	sb := mustSpawn(t, d, nil)
	res := mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"true"}})
	if res.ExitCode != 0 {
		t.Fatalf("true: %+v", res)
	}
	res, err := d.Exec(t.Context(), sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "exit 7"}})
	if err != nil {
		t.Fatalf("exit 7 driver error: %v", err)
	}
	if res.ExitCode != 7 {
		t.Fatalf("exit = %d, want 7", res.ExitCode)
	}
	_, err = d.Exec(t.Context(), sb.ID, sandbox.Cmd{Argv: nil})
	if !errors.Is(err, sandbox.ErrInvalid) {
		t.Fatalf("empty argv: %v, want ErrInvalid", err)
	}
}

func testEnvVisibility(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	sb := mustSpawn(t, d, nil)
	res := mustExec(t, d, sb.ID, sandbox.Cmd{
		Argv: []string{"sh", "-c", `printf %s "$ZEROTH_CONFORMANCE"`},
		Env:  []string{"ZEROTH_CONFORMANCE=visible"},
	})
	if res.Stdout != "visible" {
		t.Fatalf("cmd env = %q", res.Stdout)
	}
	res = mustExec(t, d, sb.ID, sandbox.Cmd{
		Argv: []string{"sh", "-c", `printf %s "$ZEROTH_CONFORMANCE"`},
	})
	if res.Stdout != "" {
		t.Fatalf("env leaked across execs: %q", res.Stdout)
	}
	res = mustExec(t, d, sb.ID, sandbox.Cmd{
		Argv: []string{"sh", "-c", `printf %s "$ZEROTH_HOST_SECRET"`},
	})
	if res.Stdout != "" {
		t.Fatalf("host env visible inside sandbox: %q", res.Stdout)
	}
	_, err := d.Exec(t.Context(), sb.ID, sandbox.Cmd{
		Argv: []string{"true"},
		Env:  []string{"NOTPAIRS"},
	})
	if !errors.Is(err, sandbox.ErrInvalid) {
		t.Fatalf("malformed env: %v, want ErrInvalid", err)
	}
}

func testHostIsolation(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	// Covers sandbox.Exec only. The Claude Code harness is a host
	// subprocess (ADR-Z-0010) and is proven in
	// claudecode.TestSpawnIsHostSubprocess, not here.
	t.Helper()
	d := open(t)
	sb := mustSpawn(t, d, nil)
	canary := filepath.Join(t.TempDir(), "host-canary")
	if err := os.WriteFile(canary, []byte("host"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = d.Exec(t.Context(), sb.ID, sandbox.Cmd{
		Argv: []string{"sh", "-c", "echo sandbox > '" + canary + "' || true"},
	})
	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "host" {
		t.Fatalf("sandbox wrote through to host canary: %q", got)
	}
}

func testOverlayWorkspace(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())
	mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo persist > /workspace/kept.txt && echo tmp > /tmp/gone.txt"}})
	var buf bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &buf); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}
	if err := d.Stop(ctx, sb.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	restored := mustSpawn(t, d, bytes.NewReader(buf.Bytes()))
	res := mustExec(t, d, restored.ID, sandbox.Cmd{Argv: []string{"cat", "kept.txt"}})
	if strings.TrimSpace(res.Stdout) != "persist" {
		t.Fatalf("workspace write missing after restore: %q", res.Stdout)
	}
	res = mustExec(t, d, restored.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "test -f /tmp/gone.txt && echo yes || echo no"}})
	if strings.TrimSpace(res.Stdout) != "no" {
		t.Fatalf("/tmp write survived restore: %q", res.Stdout)
	}
}

func testTarRoundTripHash(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())
	mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo extra > extra.txt"}})
	var first bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &first); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}
	sum := mustHashTar(t, first.Bytes())
	if sum == emptyTreeHash {
		t.Fatal("workspace hash is empty")
	}

	if err := d.Stop(ctx, sb.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	restored := mustSpawn(t, d, bytes.NewReader(first.Bytes()))
	var second bytes.Buffer
	if err := d.ExportTar(ctx, restored.ID, &second); err != nil {
		t.Fatalf("ExportTar restored: %v", err)
	}
	got := mustHashTar(t, second.Bytes())
	if got != sum {
		t.Fatalf("round-trip hash mismatch: started %s restored %s", sum, got)
	}
	res := mustExec(t, d, restored.ID, sandbox.Cmd{Argv: []string{"cat", "extra.txt"}})
	if strings.TrimSpace(res.Stdout) != "extra" {
		t.Fatalf("restored extra.txt = %q", res.Stdout)
	}
}

func testImportRejectsEscape(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	sb := mustSpawn(t, d, helloTar())
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
	err := d.ImportTar(t.Context(), sb.ID, bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, sandbox.ErrInvalid) {
		t.Fatalf("escaping tar: %v, want ErrInvalid", err)
	}
	res := mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"cat", "hello.txt"}})
	if res.Stdout != "hello sandbox\n" {
		t.Fatalf("workspace clobbered after rejected import: %q", res.Stdout)
	}
}

func testEgressDenyDefault(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	sb := mustSpawn(t, d, nil)
	res := mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "printf %s \"$HTTP_PROXY\""}})
	if strings.TrimSpace(res.Stdout) != "" {
		t.Fatalf("HTTP_PROXY set with empty allowlist: %q", res.Stdout)
	}
	res = mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"ls", "/sys/class/net"}})
	for _, iface := range strings.Fields(res.Stdout) {
		if iface != "lo" {
			t.Fatalf("deny-default has extra iface %q (%q)", iface, res.Stdout)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "leaked")
	}))
	t.Cleanup(srv.Close)
	res, err := d.Exec(t.Context(), sb.ID, sandbox.Cmd{Argv: []string{"wget", "-T", "2", "-qO-", srv.URL}})
	if err != nil {
		t.Fatalf("wget deny driver error: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("deny-default reached host listener: stdout=%q", res.Stdout)
	}
}

func testEgressAllowListed(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	sb := mustSpawn(t, d, nil)

	allowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(allowSrv.Close)
	denySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "should-not-see")
	}))
	t.Cleanup(denySrv.Close)

	allowHost, allowPort := mustHostPort(t, allowSrv.URL)
	if err := d.AllowEgress(t.Context(), sb.ID, []sandbox.EgressRule{{Host: allowHost, Port: allowPort}}); err != nil {
		t.Fatalf("AllowEgress: %v", err)
	}
	res := mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "printf %s \"$HTTP_PROXY\""}})
	if !strings.Contains(res.Stdout, "host.docker.internal:") {
		t.Fatalf("HTTP_PROXY = %q", res.Stdout)
	}

	res, err := d.Exec(t.Context(), sb.ID, sandbox.Cmd{
		Argv: []string{"wget", "-qO-", allowSrv.URL},
	})
	if err != nil {
		t.Fatalf("wget allow: %v", err)
	}
	if res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != "ok" {
		t.Fatalf("listed dest: exit=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	}

	res, err = d.Exec(t.Context(), sb.ID, sandbox.Cmd{
		Argv: []string{"wget", "-qO-", denySrv.URL},
	})
	if err != nil {
		t.Fatalf("wget deny driver error: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("unlisted dest succeeded: stdout=%q", res.Stdout)
	}
}

func testKillInflight(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())

	type execOut struct {
		res sandbox.ExecResult
		err error
	}
	done := make(chan execOut, 1)
	go func() {
		res, err := d.Exec(ctx, sb.ID, sandbox.Cmd{Argv: []string{"sleep", "30"}})
		done <- execOut{res: res, err: err}
	}()
	time.Sleep(300 * time.Millisecond)
	if err := d.Kill(ctx, sb.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case out := <-done:
		// Signal death is a non-zero exit with a nil driver error.
		// Either form means the sleep did not finish.
		if out.err == nil && out.res.ExitCode == 0 {
			t.Fatal("in-flight exec completed after kill")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("in-flight exec did not return after kill")
	}
	_, err := d.Exec(ctx, sb.ID, sandbox.Cmd{Argv: []string{"true"}})
	if !errors.Is(err, sandbox.ErrKilled) {
		t.Fatalf("exec after kill: %v, want ErrKilled", err)
	}
	var buf bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &buf); err != nil {
		t.Fatalf("ExportTar after kill: %v", err)
	}
	if mustHashTar(t, buf.Bytes()) == emptyTreeHash {
		t.Fatal("export after kill lost the workspace")
	}
}

func testKillLostWork(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, nil)
	mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo one > /workspace/one.txt"}})
	var ckpt bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &ckpt); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}
	mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo two > /workspace/two.txt"}})
	if err := d.Kill(ctx, sb.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := d.Stop(ctx, sb.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	restored := mustSpawn(t, d, bytes.NewReader(ckpt.Bytes()))
	res := mustExec(t, d, restored.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "test -f /workspace/one.txt && echo one; test -f /workspace/two.txt && echo two || true"}})
	out := strings.Fields(res.Stdout)
	if len(out) != 1 || out[0] != "one" {
		t.Fatalf("lost-work restore = %q, want only one", res.Stdout)
	}
	res = mustExec(t, d, restored.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo resumed > /workspace/resumed && cat /workspace/resumed"}})
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "resumed") {
		t.Fatalf("write after restore failed: %+v", res)
	}
}

func testKillDaemonNotRestored(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, nil)
	go func() {
		_, _ = d.Exec(ctx, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c",
			`: > /workspace/devserver.log
while true; do echo alive >> /workspace/devserver.log; sleep 1; done`}})
	}()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		res, err := d.Exec(ctx, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "test -s /workspace/devserver.log && echo yes || true"}})
		if err == nil && strings.Contains(res.Stdout, "yes") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !logGrowing(t, d, sb.ID) {
		t.Fatal("log did not grow before kill")
	}
	var ckpt bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &ckpt); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}
	if err := d.Kill(ctx, sb.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := d.Stop(ctx, sb.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	restored := mustSpawn(t, d, bytes.NewReader(ckpt.Bytes()))
	res := mustExec(t, d, restored.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "test -s /workspace/devserver.log && echo restored"}})
	if res.ExitCode != 0 {
		t.Fatal("daemon log did not restore")
	}
	if logGrowing(t, d, restored.ID) {
		t.Fatal("in-sandbox daemon resumed; checkpoint must not restore processes")
	}
}

func testExportAlongsideExec(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())
	mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "dd if=/dev/zero of=/workspace/.pad bs=1024 count=256 status=none"}})

	errc := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := d.Exec(ctx, sb.ID, sandbox.Cmd{Argv: []string{"sleep", "2"}})
		errc <- err
	}()
	var buf bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &buf); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("exec: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 3500*time.Millisecond {
		t.Fatalf("export blocked turn: elapsed=%s", elapsed)
	}
}

func testCredFilesTmpfs(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())
	tokenPath := sandbox.CredsDir + "/token"
	payload := []byte("lease-token-not-a-detector")
	res := mustExec(t, d, sb.ID, sandbox.Cmd{
		Argv:  []string{"cat", tokenPath},
		Files: []sandbox.CredFile{{Path: tokenPath, Data: payload}},
	})
	if res.Stdout != string(payload) {
		t.Fatalf("injected cred = %q", res.Stdout)
	}
	res = mustExec(t, d, sb.ID, sandbox.Cmd{
		Argv: []string{"sh", "-c", "test -f '" + tokenPath + "' && echo yes || echo no"},
	})
	if strings.TrimSpace(res.Stdout) != "no" {
		t.Fatalf("cred file leaked to next exec: %q", res.Stdout)
	}
	res = mustExec(t, d, sb.ID, sandbox.Cmd{
		Argv: []string{"sh", "-c", "test -f /workspace/token && echo yes || echo no"},
	})
	if strings.TrimSpace(res.Stdout) != "no" {
		t.Fatalf("cred file landed in workspace: %q", res.Stdout)
	}
	var buf bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &buf); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}
	if bytes.Contains(buf.Bytes(), payload) {
		t.Fatal("injected credential appeared in export")
	}
	_, err := d.Exec(ctx, sb.ID, sandbox.Cmd{
		Argv:  []string{"true"},
		Files: []sandbox.CredFile{{Path: sandbox.WorkspaceDir + "/secret", Data: payload}},
	})
	if !errors.Is(err, sandbox.ErrInvalid) {
		t.Fatalf("workspace cred path: %v, want ErrInvalid", err)
	}
}

func testCredentialExcludedFromExport(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())
	script := strings.Join([]string{
		"echo kept > /workspace/kept.txt",
		"mkdir -p /workspace/.config/gh",
		"echo 'https://user:oauth-store@github.com' > /workspace/.git-credentials",
		"echo hist > /workspace/.bash_history",
		"echo 'github.com:' > /workspace/.config/gh/hosts.yml",
		"echo 'machine github.com login user password cache' > /workspace/.netrc",
	}, " && ")
	mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", script}})
	var buf bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &buf); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}
	names := tarFileNames(t, buf.Bytes())
	if !names["kept.txt"] || !names["hello.txt"] {
		t.Fatalf("export missing workspace files: %v", names)
	}
	for _, forbidden := range []string{
		".git-credentials",
		".bash_history",
		".config/gh/hosts.yml",
		".netrc",
	} {
		if names[forbidden] {
			t.Fatalf("export contains excluded %q", forbidden)
		}
	}
	if bytes.Contains(buf.Bytes(), []byte("oauth-store")) {
		t.Fatal("excluded credential content appeared in export")
	}
}

func testCompiledMemoryExcludedFromExport(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())
	script := strings.Join([]string{
		"echo kept > /workspace/kept.txt",
		"echo compiled > /workspace/AGENTS.md",
		"echo compiled > /workspace/CLAUDE.md",
		"mkdir -p /workspace/.cursor/rules /workspace/.zeroth/compiled-memory",
		"echo rule > /workspace/.cursor/rules/zeroth-memory.mdc",
		"echo other > /workspace/.cursor/rules/other.mdc",
		"echo marker > /workspace/.zeroth/compiled-memory/README",
	}, " && ")
	mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", script}})
	var buf bytes.Buffer
	if err := d.ExportTar(ctx, sb.ID, &buf); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}
	names := tarFileNames(t, buf.Bytes())
	if !names["kept.txt"] || !names["hello.txt"] || !names[".cursor/rules/other.mdc"] {
		t.Fatalf("export missing workspace files: %v", names)
	}
	for _, forbidden := range []string{
		"AGENTS.md",
		"CLAUDE.md",
		".cursor/rules/zeroth-memory.mdc",
		".zeroth/compiled-memory/README",
	} {
		if names[forbidden] {
			t.Fatalf("export contains compiled memory %q", forbidden)
		}
	}
}

func testExportSecretFailClosed(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	sb := mustSpawn(t, d, helloTar())
	mustExec(t, d, sb.ID, sandbox.Cmd{
		Argv: []string{"sh", "-c", `printf '%s%s\n' "$A" "$B" > /workspace/notes.txt`},
		Env:  []string{"A=AKI", "B=AZEROTHTESTFAKE01"},
	})
	var buf bytes.Buffer
	err := d.ExportTar(ctx, sb.ID, &buf)
	if !errors.Is(err, sandbox.ErrSecret) {
		t.Fatalf("ExportTar: %v, want ErrSecret", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("fail-closed export wrote %d bytes", buf.Len())
	}
	mustExec(t, d, sb.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo clean > /workspace/notes.txt"}})
	buf.Reset()
	if err := d.ExportTar(ctx, sb.ID, &buf); err != nil {
		t.Fatalf("clean ExportTar: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("clean export was empty")
	}
}

func testBranchIndependent(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	parent := mustSpawn(t, d, helloTar())
	mustExec(t, d, parent.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo trunk > /workspace/trunk.txt"}})
	var ckpt bytes.Buffer
	if err := d.ExportTar(ctx, parent.ID, &ckpt); err != nil {
		t.Fatalf("ExportTar: %v", err)
	}

	left := mustSpawn(t, d, bytes.NewReader(ckpt.Bytes()))
	right := mustSpawn(t, d, bytes.NewReader(ckpt.Bytes()))
	if left.ID == right.ID {
		t.Fatal("branch sandboxes share an id")
	}
	mustExec(t, d, left.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo left > /workspace/side.txt"}})
	mustExec(t, d, right.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "echo right > /workspace/side.txt"}})

	leftTrunk := mustExec(t, d, left.ID, sandbox.Cmd{Argv: []string{"cat", "trunk.txt"}})
	rightTrunk := mustExec(t, d, right.ID, sandbox.Cmd{Argv: []string{"cat", "trunk.txt"}})
	if strings.TrimSpace(leftTrunk.Stdout) != "trunk" || strings.TrimSpace(rightTrunk.Stdout) != "trunk" {
		t.Fatalf("branch lost trunk: left=%q right=%q", leftTrunk.Stdout, rightTrunk.Stdout)
	}
	leftSide := mustExec(t, d, left.ID, sandbox.Cmd{Argv: []string{"cat", "side.txt"}})
	rightSide := mustExec(t, d, right.ID, sandbox.Cmd{Argv: []string{"cat", "side.txt"}})
	if strings.TrimSpace(leftSide.Stdout) != "left" {
		t.Fatalf("left side = %q", leftSide.Stdout)
	}
	if strings.TrimSpace(rightSide.Stdout) != "right" {
		t.Fatalf("right side = %q", rightSide.Stdout)
	}
	parentSide := mustExec(t, d, parent.ID, sandbox.Cmd{Argv: []string{"sh", "-c", "test -f /workspace/side.txt && echo yes || echo no"}})
	if strings.TrimSpace(parentSide.Stdout) != "no" {
		t.Fatalf("parent saw a branch write: %q", parentSide.Stdout)
	}
}

func tarFileNames(t *testing.T, raw []byte) map[string]bool {
	t.Helper()
	out := make(map[string]bool)
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name == "" || name == "." {
			continue
		}
		out[name] = true
	}
}

func testUnknownID(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	id, err := sandbox.ParseID("sbx_missing")
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Exec(t.Context(), id, sandbox.Cmd{Argv: []string{"true"}})
	if !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("exec missing: %v, want ErrNotFound", err)
	}
	if err := d.Kill(t.Context(), id); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("kill missing: %v", err)
	}
}

func testStopIdempotent(t *testing.T, open func(t *testing.T) sandbox.Driver) {
	t.Helper()
	d := open(t)
	sb := mustSpawn(t, d, nil)
	if err := d.Stop(t.Context(), sb.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := d.Stop(t.Context(), sb.ID); err != nil {
		t.Fatalf("Stop again: %v", err)
	}
}

func mustSpawn(t *testing.T, d sandbox.Driver, workspace io.Reader) sandbox.Sandbox {
	t.Helper()
	sb, err := d.Spawn(t.Context(), sandbox.Spec{Workspace: workspace})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = d.Stop(context.Background(), sb.ID)
	})
	return sb
}

func mustExec(t *testing.T, d sandbox.Driver, id sandbox.ID, cmd sandbox.Cmd) sandbox.ExecResult {
	t.Helper()
	res, err := d.Exec(t.Context(), id, cmd)
	if err != nil {
		t.Fatalf("Exec %v: %v", cmd.Argv, err)
	}
	return res
}

func helloTar() io.Reader {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := "hello sandbox\n"
	hdr := &tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
	if _, err := io.WriteString(tw, body); err != nil {
		panic(err)
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func logGrowing(t *testing.T, d sandbox.Driver, id sandbox.ID) bool {
	t.Helper()
	a := logBytes(t, d, id)
	time.Sleep(2 * time.Second)
	b := logBytes(t, d, id)
	return b > a
}

func logBytes(t *testing.T, d sandbox.Driver, id sandbox.ID) int {
	t.Helper()
	res, err := d.Exec(t.Context(), id, sandbox.Cmd{Argv: []string{"sh", "-c", "wc -c < /workspace/devserver.log"}})
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(res.Stdout))
	return n
}

func mustHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ToLower(host), n
}

const emptyTreeHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func mustHashTar(t *testing.T, raw []byte) string {
	t.Helper()
	dir := t.TempDir()
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("hash tar: %v", err)
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name == "" || name == "." {
			continue
		}
		target := filepath.Join(dir, filepath.Clean(name))
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != 0 {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(target, os.FileMode(hdr.Mode)&0o777)
	}
	sum, err := hashTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum[:])
}

func hashTree(root string) ([32]byte, error) {
	var out [32]byte
	var rels []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rels = append(rels, rel)
		return nil
	})
	if err != nil {
		return out, err
	}
	h := sha256.New()
	for _, rel := range rels {
		path := filepath.Join(root, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return out, err
		}
		kind := 'f'
		switch {
		case info.IsDir():
			kind = 'd'
		case info.Mode()&os.ModeSymlink != 0:
			kind = 'l'
		}
		if _, err := fmt.Fprintf(h, "%s\n%c\n%o\n", filepath.ToSlash(rel), kind, info.Mode().Perm()); err != nil {
			return out, err
		}
		if kind != 'f' {
			continue
		}
		if _, err := fmt.Fprintf(h, "%d\n", info.Size()); err != nil {
			return out, err
		}
		f, err := os.Open(path)
		if err != nil {
			return out, err
		}
		_, copyErr := io.Copy(h, f)
		_ = f.Close()
		if copyErr != nil {
			return out, copyErr
		}
	}
	copy(out[:], h.Sum(nil))
	return out, nil
}
