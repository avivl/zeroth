package server

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// recordingSandbox is an in-process Driver that records overlay files
// HydrateSandbox writes via DEST/BLOB env, so spawn hydration can be
// asserted without Docker.
type recordingSandbox struct {
	mu   sync.Mutex
	n    int
	inst map[string]*recordingInst
}

type recordingInst struct {
	files   map[string]string
	killed  bool
	stopped bool
}

func newRecordingSandbox() *recordingSandbox {
	return &recordingSandbox{inst: make(map[string]*recordingInst)}
}

func (f *recordingSandbox) Name() string { return "recording" }

func (f *recordingSandbox) Spawn(context.Context, sandbox.Spec) (sandbox.Sandbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	id, err := sandbox.ParseID("sbx_rec_" + strconv.Itoa(f.n))
	if err != nil {
		return sandbox.Sandbox{}, err
	}
	f.inst[id.String()] = &recordingInst{files: make(map[string]string)}
	return sandbox.Sandbox{ID: id}, nil
}

func (f *recordingSandbox) Exec(_ context.Context, id sandbox.ID, cmd sandbox.Cmd) (sandbox.ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
		inst.files[path.Clean(dest)] = string(data)
	}
	return sandbox.ExecResult{ExitCode: 0}, nil
}

func (f *recordingSandbox) ExportTar(_ context.Context, id sandbox.ID, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.inst[id.String()]
	if !ok {
		return sandbox.ErrNotFound
	}
	tw := tar.NewWriter(w)
	for p, body := range inst.files {
		rel := strings.TrimPrefix(p, sandbox.WorkspaceDir+"/")
		if sandbox.ExcludedFromExport(rel) {
			continue
		}
		hdr := &tar.Header{Name: rel, Mode: 0o644, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			_ = tw.Close()
			return err
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			_ = tw.Close()
			return err
		}
	}
	return tw.Close()
}

func (f *recordingSandbox) ImportTar(context.Context, sandbox.ID, io.Reader) error { return nil }
func (f *recordingSandbox) AllowEgress(context.Context, sandbox.ID, []sandbox.EgressRule) error {
	return nil
}

func (f *recordingSandbox) Kill(_ context.Context, id sandbox.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.inst[id.String()]
	if !ok {
		return sandbox.ErrNotFound
	}
	inst.killed = true
	return nil
}

func (f *recordingSandbox) Stop(_ context.Context, id sandbox.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.inst[id.String()]
	if !ok {
		return nil
	}
	inst.stopped = true
	delete(f.inst, id.String())
	return nil
}

func (f *recordingSandbox) file(id sandbox.ID, p string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.inst[id.String()]
	if !ok {
		return "", false
	}
	body, ok := inst.files[path.Clean(p)]
	return body, ok
}

func (f *recordingSandbox) anyID() sandbox.ID {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.inst {
		id, _ := sandbox.ParseID(k)
		return id
	}
	return sandbox.ID{}
}

type refuseWorld struct{}

func (refuseWorld) Observe(context.Context, string) (string, error) { return "", nil }
func (refuseWorld) Execute(context.Context, plan.Row) (string, error) {
	return "", errWorkspaceExecute
}
func (refuseWorld) Seen(string) (string, bool) { return "", false }

type staticLeaser struct{ leases []policy.Lease }

func (l staticLeaser) Acquire(context.Context, plan.Plan) ([]policy.Lease, error) {
	out := make([]policy.Lease, len(l.leases))
	copy(out, l.leases)
	return out, nil
}
func (l staticLeaser) Release(context.Context, []policy.Lease) error { return nil }

type nopCK struct{}

func (nopCK) Checkpoint(context.Context) (plan.CheckpointRef, error) { return "ckpt-mem", nil }

type nopAudit struct{}

func (nopAudit) SignRow(context.Context, plan.Row, string) error { return nil }
func (nopAudit) SignPlan(context.Context, plan.Plan) error       { return nil }

var errWorkspaceExecute = errString("workspace execute must not run for memory_proposal")

type errString string

func (e errString) Error() string { return string(e) }

func TestSpawnHydratesNotebookAndApplyProposes(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	fs := newRecordingSandbox()
	srv, err := New(Config{
		Store:         st,
		Sandbox:       fs,
		TokenInterval: time.Second,
		TokenCount:    1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	nb := srv.notebook()
	if _, err := nb.Write(t.Context(), memory.Human("operator"), memory.KindOperator, "", "style.tests", "prefer table tests", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := nb.Write(t.Context(), memory.Human("operator"), memory.KindSession, "s_other", "secret.other", "must not leak", "operator"); err != nil {
		t.Fatal(err)
	}

	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	prompt := "hydrate me"
	body, _ := json.Marshal(gen.CreateRunRequest{
		AgentId: gen.AgentID(DefaultAgentID),
		Prompt:  &prompt,
	})
	res, err := http.Post(hs.URL+"/runs", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("create %d %s", res.StatusCode, slurp)
	}
	var run gen.Run
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}

	sbx := fs.anyID()
	if sbx.IsZero() {
		t.Fatal("create-run did not spawn a sandbox")
	}
	agents, ok := fs.file(sbx, sandbox.WorkspaceDir+"/"+memory.CompiledAgents)
	if !ok {
		t.Fatal("sandbox missing AGENTS.md after spawn")
	}
	if !strings.Contains(agents, "prefer table tests") {
		t.Fatalf("AGENTS.md missing notebook slice:\n%s", agents)
	}
	if strings.Contains(agents, "must not leak") {
		t.Fatalf("AGENTS.md leaked another session:\n%s", agents)
	}
	if _, ok := fs.file(sbx, sandbox.WorkspaceDir+"/"+memory.CompiledClaude); !ok {
		t.Fatal("sandbox missing CLAUDE.md")
	}

	var tarBuf bytes.Buffer
	if err := fs.ExportTar(t.Context(), sbx, &tarBuf); err != nil {
		t.Fatal(err)
	}
	names := tarEntryNames(t, tarBuf.Bytes())
	for _, forbidden := range memory.CompiledPaths() {
		if names[forbidden] {
			t.Fatalf("checkpoint contains compiled %q: %v", forbidden, names)
		}
	}

	sid, err := store.ParseSessionID(string(run.Id))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.GetSession(t.Context(), sid)
	if err != nil {
		t.Fatal(err)
	}
	planID, err := plan.NewID()
	if err != nil {
		t.Fatal(err)
	}
	sessID, err := session.ParseID(sess.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	draft, err := plan.Build(plan.Draft{
		ID:        planID,
		SessionID: sessID,
		Summary:   "remember the house style",
		Effects:   []plan.Proposed{{Type: "memory_proposal", Path: "session/style", Diff: "use table tests"}},
		Lease:     "lease-1",
		ExpiresAt: now.Add(time.Hour),
		Scope:     "scope-a",
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	draft.Status = plan.StatusPendingApproval
	draft.CrossExam = &plan.CrossExam{Verdict: plan.VerdictPass, ReviewerModel: "test-reviewer", At: now}
	approved, err := draft.Approve(now)
	if err != nil {
		t.Fatal(err)
	}

	applier := &plan.Applier{
		Kernel:      policy.New(),
		World:       refuseWorld{},
		Leases:      staticLeaser{leases: []policy.Lease{policy.NewLease("lease-1", "scope-a", "alice", now.Add(time.Hour), plan.OpMemoryProposal.Kind())}},
		Checkpoints: nopCK{},
		Audit:       nopAudit{},
		Memory:      srv.memoryQueue(sess),
	}
	got, err := applier.Apply(t.Context(), "alice", approved, plan.Approval{PlanHash: approved.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != plan.StatusApplied || got.AppliedThrough != 1 {
		t.Fatalf("status=%q through=%d", got.Status, got.AppliedThrough)
	}

	pending, err := nb.ListProposals(t.Context(), memory.StatusPending)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending %+v", pending)
	}
	if pending[0].Key != "style" || pending[0].Body != "use table tests" || pending[0].Kind != memory.KindSession {
		t.Fatalf("proposal %+v", pending[0])
	}
	if pending[0].SessionID != sess.ID {
		t.Fatalf("proposal session %s want %s", pending[0].SessionID, sess.ID)
	}
	if pending[0].Status != memory.StatusPending {
		t.Fatalf("status %q, agent accepted a fact", pending[0].Status)
	}
	slice, err := nb.Slice(t.Context(), memory.KindSession, sess.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range slice {
		if f.Key == "style" {
			t.Fatal("proposal leaked into the notebook without accept")
		}
	}
}

func tarEntryNames(t *testing.T, raw []byte) map[string]bool {
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
