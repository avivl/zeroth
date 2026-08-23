package server_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"github.com/avivl/zeroth/internal/tracker"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

type stubTracker struct {
	mu         sync.Mutex
	ch         chan tracker.AssignmentEvent
	comments   []string
	states     []tracker.StateKind
	artifacts  []tracker.Artifact
	unassigned []string
	issue      tracker.Issue
	issues     map[string]tracker.Issue
	threads    map[string][]tracker.Comment
}

func newStubTracker(iss tracker.Issue) *stubTracker {
	s := &stubTracker{
		ch:      make(chan tracker.AssignmentEvent, 8),
		issue:   iss,
		issues:  make(map[string]tracker.Issue),
		threads: make(map[string][]tracker.Comment),
	}
	s.putIssueLocked(iss)
	return s
}

func (s *stubTracker) putIssueLocked(iss tracker.Issue) {
	if iss.Key != "" {
		s.issues[iss.Key] = iss
	}
	if iss.ID != "" {
		s.issues[iss.ID] = iss
	}
}

func (s *stubTracker) putIssue(iss tracker.Issue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putIssueLocked(iss)
}

func (s *stubTracker) putThread(key string, comments []tracker.Comment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[key] = append([]tracker.Comment(nil), comments...)
}

func (s *stubTracker) Name() string { return "stub" }
func (s *stubTracker) Capabilities() tracker.Capabilities {
	return tracker.Capabilities{}
}
func (s *stubTracker) GetIssue(_ context.Context, key string) (tracker.Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	iss, ok := s.issues[key]
	if !ok {
		return tracker.Issue{}, tracker.ErrNotFound
	}
	return iss, nil
}
func (s *stubTracker) ListComments(_ context.Context, key string) ([]tracker.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.issues[key]; !ok {
		return nil, tracker.ErrNotFound
	}
	return append([]tracker.Comment(nil), s.threads[key]...), nil
}
func (s *stubTracker) Comment(_ context.Context, _, body string) (tracker.CommentRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments = append(s.comments, body)
	return tracker.CommentRef{ID: "c1"}, nil
}
func (s *stubTracker) SetState(_ context.Context, _ string, state tracker.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states = append(s.states, state.Kind)
	return nil
}
func (s *stubTracker) Assignments(context.Context) (<-chan tracker.AssignmentEvent, error) {
	return s.ch, nil
}
func (s *stubTracker) LinkArtifact(_ context.Context, _ string, a tracker.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts = append(s.artifacts, a)
	return nil
}
func (s *stubTracker) Unassign(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unassigned = append(s.unassigned, key)
	return nil
}
func (s *stubTracker) commentBodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.comments...)
	return out
}
func (s *stubTracker) lastState() tracker.StateKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.states) == 0 {
		return ""
	}
	return s.states[len(s.states)-1]
}

func (s *stubTracker) artifactURLs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.artifacts))
	for _, a := range s.artifacts {
		out = append(out, a.URL)
	}
	return out
}

func (s *stubTracker) unassignKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.unassigned...)
}

type fakeSandbox struct {
	mu       sync.Mutex
	n        int
	seed     map[string]string
	inst     map[string]*fakeInst
	execArgv [][]string
}

type fakeInst struct {
	workspace string
	killed    bool
	stopped   bool
}

func newFakeSandbox() *fakeSandbox {
	return &fakeSandbox{inst: make(map[string]*fakeInst)}
}

func newSeededSandbox(files map[string]string) *fakeSandbox {
	return &fakeSandbox{seed: files, inst: make(map[string]*fakeInst)}
}

func (f *fakeSandbox) Name() string { return "fake" }

func (f *fakeSandbox) Spawn(context.Context, sandbox.Spec) (sandbox.Sandbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	id, err := sandbox.ParseID("sbx_test_" + strconv.Itoa(f.n))
	if err != nil {
		id, _ = sandbox.NewID()
	}
	dir, err := os.MkdirTemp("", "zeroth-fake-sbx-")
	if err != nil {
		return sandbox.Sandbox{}, err
	}
	for rel, body := range f.seed {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			_ = os.RemoveAll(dir)
			return sandbox.Sandbox{}, err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			_ = os.RemoveAll(dir)
			return sandbox.Sandbox{}, err
		}
	}
	f.inst[id.String()] = &fakeInst{workspace: dir}
	return sandbox.Sandbox{ID: id}, nil
}

func (f *fakeSandbox) HostWorkspace(id sandbox.ID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.inst[id.String()]
	if !ok {
		return "", sandbox.ErrNotFound
	}
	if inst.stopped {
		return "", sandbox.ErrStopped
	}
	if inst.workspace == "" {
		return "", sandbox.ErrNotFound
	}
	return inst.workspace, nil
}

func (f *fakeSandbox) Exec(_ context.Context, id sandbox.ID, cmd sandbox.Cmd) (sandbox.ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execArgv = append(f.execArgv, append([]string(nil), cmd.Argv...))
	inst, ok := f.inst[id.String()]
	if !ok {
		return sandbox.ExecResult{}, sandbox.ErrNotFound
	}
	if inst.stopped {
		return sandbox.ExecResult{}, sandbox.ErrStopped
	}
	if inst.killed {
		return sandbox.ExecResult{}, sandbox.ErrKilled
	}
	dest, blob := "", ""
	for _, e := range cmd.Env {
		k, v, _ := strings.Cut(e, "=")
		switch k {
		case "DEST":
			dest = v
		case "BLOB":
			blob = v
		}
	}
	if dest != "" && blob != "" {
		data, err := base64.StdEncoding.DecodeString(blob)
		if err != nil {
			return sandbox.ExecResult{ExitCode: 1, Stderr: err.Error()}, nil
		}
		rel := strings.TrimPrefix(dest, sandbox.WorkspaceDir+"/")
		path := filepath.Join(inst.workspace, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return sandbox.ExecResult{ExitCode: 1, Stderr: err.Error()}, nil
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return sandbox.ExecResult{ExitCode: 1, Stderr: err.Error()}, nil
		}
	}
	return sandbox.ExecResult{ExitCode: 0, Stdout: "alive\n"}, nil
}

func (f *fakeSandbox) ExportTar(context.Context, sandbox.ID, io.Writer) error { return nil }
func (f *fakeSandbox) ImportTar(context.Context, sandbox.ID, io.Reader) error { return nil }
func (f *fakeSandbox) AllowEgress(context.Context, sandbox.ID, []sandbox.EgressRule) error {
	return nil
}

func (f *fakeSandbox) Kill(_ context.Context, id sandbox.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.inst[id.String()]
	if !ok {
		return sandbox.ErrNotFound
	}
	if inst.stopped {
		return sandbox.ErrStopped
	}
	inst.killed = true
	return nil
}

func (f *fakeSandbox) Stop(_ context.Context, id sandbox.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.inst[id.String()]
	if !ok {
		return nil
	}
	inst.stopped = true
	inst.killed = true
	dir := inst.workspace
	delete(f.inst, id.String())
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	return nil
}

func (f *fakeSandbox) liveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.inst)
}

func (f *fakeSandbox) anyID() sandbox.ID {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.inst {
		id, _ := sandbox.ParseID(k)
		return id
	}
	return sandbox.ID{}
}

func (f *fakeSandbox) overlayFiles(rel string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, inst := range f.inst {
		if inst.workspace == "" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(inst.workspace, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		out = append(out, string(body))
	}
	return out
}

func (f *fakeSandbox) execCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.execArgv))
	for i, argv := range f.execArgv {
		out[i] = append([]string(nil), argv...)
	}
	return out
}

func TestAssignStartsHeadlessRun(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{
		Key:         "42-1",
		ID:          "iss_1",
		Title:       "tracker.Provider",
		Description: "Assign to Zeroth",
		Project:     "Zeroth",
	}
	tr := newStubTracker(iss)
	sbx := newFakeSandbox()
	hs := assignServer(t, tr, sbx, 5*time.Millisecond, 4)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-1", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-1")
	if run.Prompt == nil || *run.Prompt == "" {
		t.Fatal("prompt empty")
	}
	if sbx.liveCount() != 1 {
		t.Fatalf("sandbox live = %d, want 1", sbx.liveCount())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bodies := tr.commentBodies()
		if len(bodies) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(tr.commentBodies()) == 0 {
		t.Fatal("expected started comment")
	}
	if tr.lastState() != tracker.StateStarted && tr.lastState() != tracker.StateCompleted {
		t.Fatalf("state = %q", tr.lastState())
	}
}

func TestUnassignCancelsAndKillsSandbox(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{Key: "42-2", Title: "cancel me"}
	tr := newStubTracker(iss)
	sbx := newFakeSandbox()
	hs := assignServer(t, tr, sbx, 50*time.Millisecond, 1000)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-2", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-2")
	id := sbx.anyID()
	if id.IsZero() {
		t.Fatal("no sandbox")
	}
	if _, err := sbx.Exec(t.Context(), id, sandbox.Cmd{Argv: []string{"true"}}); err != nil {
		t.Fatalf("sandbox should be alive: %v", err)
	}

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Unassigned, Key: "42-2", Issue: iss, At: time.Now()}
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)

	if sbx.liveCount() != 0 {
		t.Fatalf("sandbox still live after unassign: %d", sbx.liveCount())
	}
	_, err := sbx.Exec(t.Context(), id, sandbox.Cmd{Argv: []string{"true"}})
	if !errors.Is(err, sandbox.ErrNotFound) && !errors.Is(err, sandbox.ErrStopped) && !errors.Is(err, sandbox.ErrKilled) {
		t.Fatalf("exec after unassign = %v, want sandbox dead", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	foundCancel := false
	for time.Now().Before(deadline) {
		for _, c := range tr.commentBodies() {
			if containsAll(c, "cancelled", "sandbox") {
				foundCancel = true
			}
		}
		if foundCancel {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !foundCancel {
		t.Fatalf("missing cancel comment: %v", tr.commentBodies())
	}
}

func TestAssignStandinFailsWithoutPlan(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{Key: "42-3", Title: "finish me", Description: "do not dump this"}
	tr := newStubTracker(iss)
	sbx := newFakeSandbox()
	hs := assignServer(t, tr, sbx, 5*time.Millisecond, 2)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-3", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-3")
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)

	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		for _, c := range tr.commentBodies() {
			if containsAll(c, "Zeroth failed", "no plan") {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !found {
		t.Fatalf("missing fail comment: %v", tr.commentBodies())
	}
}

func assignServer(t *testing.T, tr tracker.Provider, sbx sandbox.Driver, interval time.Duration, tokens int) *httptest.Server {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: interval,
		TokenCount:    tokens,
		Tracker:       tr,
		Sandbox:       sbx,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs
}

func waitRunByTracker(t *testing.T, base, key string) gen.Run {
	t.Helper()
	return waitNewRunByTracker(t, base, key, "")
}

func waitNewRunByTracker(t *testing.T, base, key, notID string) gen.Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/runs")
		if err != nil {
			t.Fatal(err)
		}
		var list gen.RunList
		if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
			res.Body.Close()
			t.Fatal(err)
		}
		res.Body.Close()
		for _, run := range list.Items {
			if run.TrackerRef != nil && *run.TrackerRef == key && string(run.Id) != notID {
				return run
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if notID == "" {
		t.Fatalf("timed out waiting for tracker_ref %s", key)
	}
	t.Fatalf("timed out waiting for a new run on tracker_ref %s (not %s)", key, notID)
	return gen.Run{}
}

func waitRunStatus(t *testing.T, base, id string, want gen.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last gen.RunStatus
	for time.Now().Before(deadline) {
		run := getRun(t, base, id)
		last = run.Status
		if run.Status == want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("run %s status = %s, want %s", id, last, want)
}

func waitRunPlan(t *testing.T, base, id string) gen.Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last gen.Run
	for time.Now().Before(deadline) {
		last = getRun(t, base, id)
		if last.Status == gen.RunStatusFailed {
			t.Fatalf("run %s failed before attaching a plan", id)
		}
		if last.Status == gen.RunStatusWaitingApproval && last.PlanId != nil {
			return last
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("run %s status=%s plan_id=%v, want waiting_approval with plan_id", id, last.Status, last.PlanId)
	return last
}

func containsAll(s string, parts ...string) bool {
	low := strings.ToLower(s)
	for _, p := range parts {
		if !strings.Contains(low, strings.ToLower(p)) {
			return false
		}
	}
	return true
}
