package server_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"github.com/avivl/zeroth/internal/tracker"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

type stubHarness struct {
	mu       sync.Mutex
	starts   int
	lastSpec harness.Spec
	specs    []harness.Spec
	startErr error
	events   []harness.Event
}

func (h *stubHarness) Name() string { return "stub" }

func (h *stubHarness) Start(_ context.Context, spec harness.Spec) (harness.Handle, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.starts++
	h.lastSpec = spec
	h.specs = append(h.specs, spec)
	if h.startErr != nil {
		return harness.Handle{}, h.startErr
	}
	id, err := harness.NewID()
	if err != nil {
		return harness.Handle{}, err
	}
	return harness.Handle{ID: id}, nil
}

func (h *stubHarness) Stream(context.Context, harness.ID) (<-chan harness.Event, error) {
	h.mu.Lock()
	evs := append([]harness.Event(nil), h.events...)
	h.mu.Unlock()
	ch := make(chan harness.Event, len(evs)+1)
	go func() {
		for _, ev := range evs {
			ch <- ev
		}
		close(ch)
	}()
	return ch, nil
}

func (h *stubHarness) Steer(context.Context, harness.ID, string) error { return nil }

func (h *stubHarness) Checkpoint(context.Context, harness.ID) (harness.Checkpoint, error) {
	return harness.Checkpoint{}, nil
}

func (h *stubHarness) Stop(context.Context, harness.ID) error { return nil }

func (h *stubHarness) startCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.starts
}

func (h *stubHarness) prompts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.specs))
	for _, spec := range h.specs {
		out = append(out, spec.Prompt)
	}
	return out
}

func waitHarnessStarts(t *testing.T, h *stubHarness, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.startCount() >= n {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("harness starts = %d, want >= %d", h.startCount(), n)
}

func TestHarnessDraftsPlanOnceAndInboxShowsIt(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{
		Key:         "42-48",
		Title:       "document assign-to-Zeroth",
		Description: "this body must not be logged ten times a second",
	}
	tr := newStubTracker(iss)
	h := &stubHarness{
		events: []harness.Event{
			{Kind: harness.EventToken, Payload: "drafting"},
			{Kind: harness.EventToolCall, Payload: `{"name":"Read","path":"README.md"}`},
			{Kind: harness.EventEffects, Effects: []harness.Effect{
				{Type: "create", Path: "docs/linear-setup.md", Diff: "+setup steps"},
			}},
			{Kind: harness.EventExited, Payload: "0"},
		},
	}
	hs := harnessAssignServer(t, tr, newFakeSandbox(), h)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-48", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-48")
	got := waitRunPlan(t, hs.URL, string(run.Id))

	if h.startCount() != 1 {
		t.Fatalf("harness starts = %d, want 1", h.startCount())
	}

	evs := replayEvents(t, hs.URL, string(run.Id), 50)
	var tokens []string
	promptHits := 0
	for _, ev := range evs {
		if ev.Message == nil {
			continue
		}
		if strings.HasPrefix(*ev.Message, "token-") {
			t.Fatalf("stand-in token leaked into harness run: %s", *ev.Message)
		}
		if strings.Contains(*ev.Message, iss.Description) {
			promptHits++
		}
		if ev.Type == "log" {
			tokens = append(tokens, *ev.Message)
		}
	}
	if promptHits != 0 {
		t.Fatalf("issue body appeared in live output %d times", promptHits)
	}
	if len(tokens) == 0 || tokens[0] != "drafting" {
		t.Fatalf("tokens = %v, want harness payload", tokens)
	}

	inbox, err := http.Get(hs.URL + "/approvals?status=pending")
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Body.Close()
	var approvals gen.ApprovalList
	if err := json.NewDecoder(inbox.Body).Decode(&approvals); err != nil {
		t.Fatal(err)
	}
	if len(approvals.Items) != 1 || approvals.Items[0].PlanId == nil || *approvals.Items[0].PlanId != *got.PlanId {
		t.Fatalf("inbox %+v plan %v", approvals.Items, got.PlanId)
	}

	deadline := time.Now().Add(3 * time.Second)
	foundPlan := false
	for time.Now().Before(deadline) {
		for _, c := range tr.commentBodies() {
			if strings.Contains(c, "### Zeroth plan") {
				foundPlan = true
			}
		}
		if foundPlan {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !foundPlan {
		t.Fatalf("missing plan comment: %v", tr.commentBodies())
	}
}

func TestHarnessNoEffectsFailsLoudly(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{Key: "42-48b", Title: "no effects"}
	tr := newStubTracker(iss)
	h := &stubHarness{
		events: []harness.Event{
			{Kind: harness.EventToken, Payload: "thinking"},
			{Kind: harness.EventExited, Payload: "0"},
		},
	}
	hs := harnessAssignServer(t, tr, newFakeSandbox(), h)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-48b", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-48b")
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)

	if h.startCount() != 1 {
		t.Fatalf("harness starts = %d, want 1", h.startCount())
	}
	got := getRun(t, hs.URL, string(run.Id))
	if got.PlanId != nil {
		t.Fatalf("unexpected plan %s", *got.PlanId)
	}
	found := false
	for _, c := range tr.commentBodies() {
		if containsAll(c, "Zeroth failed", "without proposing a plan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing fail comment: %v", tr.commentBodies())
	}
	evs := replayEvents(t, hs.URL, string(run.Id), 50)
	saw := false
	for _, ev := range evs {
		if ev.Type == "error" && ev.Message != nil && strings.Contains(*ev.Message, "without proposing a plan") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("live output missing diagnosable error: %+v", evs)
	}
}

func TestHarnessStartErrorFailsRun(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{Key: "42-48c", Title: "no key"}
	tr := newStubTracker(iss)
	h := &stubHarness{startErr: fmt.Errorf("harness claudecode: ANTHROPIC_API_KEY is not set")}
	hs := harnessAssignServer(t, tr, newFakeSandbox(), h)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-48c", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-48c")
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)
	if h.startCount() != 1 {
		t.Fatalf("harness starts = %d, want 1", h.startCount())
	}
}

func TestHarnessModifyObservesPrecondition(t *testing.T) {
	t.Parallel()
	body := []byte("current README\n")
	iss := tracker.Issue{Key: "42-49", Title: "document assign-to-Zeroth"}
	tr := newStubTracker(iss)
	h := &stubHarness{
		events: []harness.Event{
			{Kind: harness.EventEffects, Effects: []harness.Effect{
				{Type: "modify", Path: "README.md", Diff: "-current README\n+updated README"},
			}},
			{Kind: harness.EventExited, Payload: "0"},
		},
	}
	hs := harnessAssignServer(t, tr, newSeededSandbox(map[string]string{"README.md": string(body)}), h)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-49", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-49")
	got := waitRunPlan(t, hs.URL, string(run.Id))
	p := getPlan(t, hs.URL, string(*got.PlanId))
	if len(p.Effects) != 1 || p.Effects[0].PreconditionHash == nil {
		t.Fatalf("effects %+v", p.Effects)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	if *p.Effects[0].PreconditionHash != want {
		t.Fatalf("precondition %q, want %q", *p.Effects[0].PreconditionHash, want)
	}
}

func TestHarnessModifyMissingFileFailsLoudly(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{Key: "42-49b", Title: "modify missing"}
	tr := newStubTracker(iss)
	h := &stubHarness{
		events: []harness.Event{
			{Kind: harness.EventEffects, Effects: []harness.Effect{
				{Type: "modify", Path: "README.md", Diff: "+docs"},
			}},
			{Kind: harness.EventExited, Payload: "0"},
		},
	}
	hs := harnessAssignServer(t, tr, newFakeSandbox(), h)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-49b", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-49b")
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)

	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		for _, c := range tr.commentBodies() {
			if strings.Contains(c, "could not observe workspace at draft time") &&
				strings.Contains(c, "README.md") &&
				!strings.Contains(c, "no precondition observed at draft time") {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !found {
		t.Fatalf("missing diagnosable observe failure: %v", tr.commentBodies())
	}
}

func TestHarnessBrokenHostWorkspaceFailsLoudly(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{Key: "42-49c", Title: "broken overlay"}
	tr := newStubTracker(iss)
	h := &stubHarness{
		events: []harness.Event{
			{Kind: harness.EventEffects, Effects: []harness.Effect{
				{Type: "modify", Path: "README.md", Diff: "+docs"},
			}},
		},
	}
	hs := harnessAssignServer(t, tr, &brokenOverlay{fakeSandbox: newFakeSandbox()}, h)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-49c", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-49c")
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)
	if h.startCount() != 0 {
		t.Fatalf("harness should not start without a workspace, starts = %d", h.startCount())
	}
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		for _, c := range tr.commentBodies() {
			if strings.Contains(c, "host workspace") && strings.Contains(c, "overlay missing") &&
				!strings.Contains(c, "no precondition observed at draft time") {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !found {
		t.Fatalf("missing diagnosable workspace error: %v", tr.commentBodies())
	}
}

func TestHarnessSeedsWorkspaceRootIntoOverlay(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := []byte("host checkout README\n")
	if err := os.WriteFile(filepath.Join(root, "README.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	iss := tracker.Issue{Key: "42-49d", Title: "docs from host checkout"}
	tr := newStubTracker(iss)
	h := &stubHarness{
		events: []harness.Event{
			{Kind: harness.EventEffects, Effects: []harness.Effect{
				{Type: "modify", Path: "README.md", Diff: "-host checkout README\n+updated README"},
			}},
			{Kind: harness.EventExited, Payload: "0"},
		},
	}
	hs := harnessAssignServerCfg(t, tr, newFakeSandbox(), h, root)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-49d", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-49d")
	got := waitRunPlan(t, hs.URL, string(run.Id))
	p := getPlan(t, hs.URL, string(*got.PlanId))
	if len(p.Effects) != 1 || p.Effects[0].PreconditionHash == nil {
		t.Fatalf("effects %+v", p.Effects)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	if *p.Effects[0].PreconditionHash != want {
		t.Fatalf("precondition %q, want %q", *p.Effects[0].PreconditionHash, want)
	}
}

type brokenOverlay struct {
	*fakeSandbox
}

func (b *brokenOverlay) HostWorkspace(sandbox.ID) (string, error) {
	return "", fmt.Errorf("overlay missing")
}

func getPlan(t *testing.T, base, id string) gen.Plan {
	t.Helper()
	res, err := http.Get(base + "/plans/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get plan %d", res.StatusCode)
	}
	var p gen.Plan
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStandinDoesNotDumpPrompt(t *testing.T) {
	t.Parallel()
	prompt := "Linear 42-43: unique-body-must-not-repeat"
	hs := testServer(t)
	run := createRun(t, hs.URL, prompt)
	waitTokens(t, hs.URL, string(run.Id), 2)
	evs := replayEvents(t, hs.URL, string(run.Id), 50)
	for _, ev := range evs {
		if ev.Message != nil && strings.Contains(*ev.Message, "unique-body-must-not-repeat") {
			t.Fatalf("stand-in dumped the prompt into live output: %s", *ev.Message)
		}
	}
}

func harnessAssignServer(t *testing.T, tr tracker.Provider, sbx sandbox.Driver, h harness.Driver) *httptest.Server {
	t.Helper()
	return harnessAssignServerCfg(t, tr, sbx, h, "")
}

func harnessAssignServerCfg(t *testing.T, tr tracker.Provider, sbx sandbox.Driver, h harness.Driver, workspaceRoot string) *httptest.Server {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		Tracker:       tr,
		Sandbox:       sbx,
		Harness:       h,
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs
}
