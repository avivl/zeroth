package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"go.uber.org/zap"
)

func TestObserveWorkspaceHashesExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := []byte("hello README\n")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := observeWorkspace(dir, []plan.Proposed{
		{Type: "modify", Path: "README.md", Diff: "+docs"},
		{Type: "create", Path: "docs/new.md", Diff: "+new"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	if got["README.md"] != want {
		t.Fatalf("observed README.md = %q, want %q", got["README.md"], want)
	}
	if _, ok := got["docs/new.md"]; ok {
		t.Fatal("create target should be absent from Observed")
	}
}

func TestObserveWorkspaceModifyMissingFileIsDiagnosable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, _, err := observeWorkspace(dir, []plan.Proposed{
		{Type: "modify", Path: "README.md", Diff: "+docs"},
	})
	if err == nil {
		t.Fatal("expected observe error")
	}
	msg := err.Error()
	if strings.Contains(msg, "no precondition observed at draft time") {
		t.Fatalf("opaque builder reason leaked: %s", msg)
	}
	if !strings.Contains(msg, "could not observe workspace at draft time") {
		t.Fatalf("missing observe prefix: %s", msg)
	}
	if !strings.Contains(msg, "README.md") || !strings.Contains(msg, dir) {
		t.Fatalf("error should name path and root: %s", msg)
	}
}

func TestObserveWorkspaceEmptyRootIsDiagnosable(t *testing.T) {
	t.Parallel()
	_, _, err := observeWorkspace("  ", []plan.Proposed{{Type: "modify", Path: "README.md", Diff: "x"}})
	if err == nil {
		t.Fatal("expected empty-root error")
	}
	if !strings.Contains(err.Error(), "could not observe workspace at draft time") {
		t.Fatalf("err %v", err)
	}
}

func TestPrepareWorkspaceUsesHostOverlay(t *testing.T) {
	t.Parallel()
	overlay := t.TempDir()
	sbx, err := sandbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		log:     zap.NewNop(),
		sandbox: &stubOverlay{dir: overlay},
		sandboxes: map[string]sandbox.ID{
			id.String(): sbx,
		},
	}
	dir, cleanup, err := s.prepareWorkspace(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if dir != overlay {
		t.Fatalf("workspace %q, want overlay %q", dir, overlay)
	}
}

func TestPrepareWorkspaceHostOverlayErrorIsDiagnosable(t *testing.T) {
	t.Parallel()
	sbx, err := sandbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		log:     zap.NewNop(),
		sandbox: &stubOverlay{err: errors.New("overlay missing")},
		sandboxes: map[string]sandbox.ID{
			id.String(): sbx,
		},
	}
	_, _, err = s.prepareWorkspace(t.Context(), id)
	if err == nil {
		t.Fatal("expected host workspace error")
	}
	msg := err.Error()
	if strings.Contains(msg, "no precondition observed at draft time") {
		t.Fatalf("opaque builder reason leaked: %s", msg)
	}
	if !strings.Contains(msg, "host workspace") || !strings.Contains(msg, "overlay missing") {
		t.Fatalf("err %v", err)
	}
}

func TestPrepareWorkspaceMissingSandboxRecordIsDiagnosable(t *testing.T) {
	t.Parallel()
	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		log:       zap.NewNop(),
		sandbox:   &stubOverlay{dir: t.TempDir()},
		sandboxes: map[string]sandbox.ID{},
	}
	_, _, err = s.prepareWorkspace(t.Context(), id)
	if err == nil {
		t.Fatal("expected missing sandbox record error")
	}
	if !strings.Contains(err.Error(), "no sandbox record") {
		t.Fatalf("err %v", err)
	}
}

func TestCopyWorkspaceSeedsTrackedFiles(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("docs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "docs", "guide.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".npmrc"), []byte("//registry.npmjs.org/:_authToken=secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := copyWorkspace(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("copied %d files, want at least README and docs/guide.md", n)
	}
	got, err := os.ReadFile(filepath.Join(dest, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "docs" {
		t.Fatalf("README.md %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, ".npmrc")); err == nil {
		t.Fatal("credential file should not be copied")
	}
}

func TestOverlaySourcePrefersLocalWorkspaceRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := &Server{workspaceRoot: "/tmp/not-used"}
	got := s.overlaySource(store.Session{Workspace: store.WorkspaceSource{Repo: dir}})
	if got != dir {
		t.Fatalf("source %q, want %q", got, dir)
	}
}

func TestSeedOverlayCopiesHostCheckout(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	sbx, err := sandbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		log:           zap.NewNop(),
		sandbox:       &stubOverlay{dir: dest},
		workspaceRoot: src,
	}
	if err := s.seedOverlay(sbx, store.Session{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Fatalf("README.md %q", got)
	}
}

func TestAttachPlanSurvivesConcurrentSync(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s, err := New(Config{Store: st, Log: zap.NewNop()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		t.Fatal(err)
	}
	aid, err := store.ParseAgentID(DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.CreateSession(t.Context(), store.Session{
		ID:        sid,
		AgentID:   aid,
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.sup.StartWith(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	pid, err := store.ParsePlanID("plan_race")
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = s.syncSession(ctx, id)
				}
			}
		}()
	}
	if err := s.attachPlan(ctx, id, pid, now); err != nil {
		close(stop)
		wg.Wait()
		t.Fatal(err)
	}
	close(stop)
	wg.Wait()
	for i := 0; i < 20; i++ {
		if err := s.syncSession(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanID != pid {
		t.Fatalf("plan_id=%q, want %q (syncSession dropped the attach)", got.PlanID.String(), pid.String())
	}
}

type stubOverlay struct {
	dir string
	err error
}

func (s *stubOverlay) Name() string { return "stub-overlay" }

func (s *stubOverlay) Spawn(context.Context, sandbox.Spec) (sandbox.Sandbox, error) {
	return sandbox.Sandbox{}, nil
}
func (s *stubOverlay) Exec(context.Context, sandbox.ID, sandbox.Cmd) (sandbox.ExecResult, error) {
	return sandbox.ExecResult{}, nil
}
func (s *stubOverlay) ExportTar(context.Context, sandbox.ID, io.Writer) error { return nil }
func (s *stubOverlay) ImportTar(context.Context, sandbox.ID, io.Reader) error { return nil }
func (s *stubOverlay) AllowEgress(context.Context, sandbox.ID, []sandbox.EgressRule) error {
	return nil
}
func (s *stubOverlay) Kill(context.Context, sandbox.ID) error   { return nil }
func (s *stubOverlay) Stop(context.Context, sandbox.ID) error   { return nil }
func (s *stubOverlay) HostWorkspace(sandbox.ID) (string, error) { return s.dir, s.err }

var _ sandbox.Driver = (*stubOverlay)(nil)
