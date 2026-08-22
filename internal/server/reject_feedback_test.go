package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"github.com/avivl/zeroth/internal/tracker"
	"github.com/avivl/zeroth/internal/tracker/linear"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

const rejectCorrection = "that heading doesn't exist, use the real one"

func TestRejectCommentFeedsNextHarnessPrompt(t *testing.T) {
	t.Parallel()
	iss := tracker.Issue{
		Key:         "42-54",
		Title:       "fix the docs heading",
		Description: "edit README under Docs",
	}
	tr := newStubTracker(iss)
	h := newRejectHarness()
	hs := harnessAssignServer(t, tr, newFakeSandbox(), h)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-54", Issue: iss, At: time.Now()}
	run := waitRunByTracker(t, hs.URL, "42-54")
	got := waitRunPlan(t, hs.URL, string(run.Id))
	waitHarnessStarts(t, h, 1)
	if strings.Contains(strings.Join(h.prompts(), "\n"), rejectCorrection) {
		t.Fatal("first prompt already had the correction")
	}

	res := postJSON(t, hs.URL+"/plans/"+string(*got.PlanId)+"/request-changes", gen.RequestChangesRequest{
		Comment: rejectCorrection,
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("request-changes %d %s", res.StatusCode, slurp)
	}
	var rejected gen.Plan
	if err := json.NewDecoder(res.Body).Decode(&rejected); err != nil {
		t.Fatal(err)
	}
	if rejected.Status != gen.PlanStatusChangesRequested {
		t.Fatalf("status %s", rejected.Status)
	}
	if rejected.ReviewComment == nil || *rejected.ReviewComment != rejectCorrection {
		t.Fatalf("review_comment %+v", rejected.ReviewComment)
	}

	waitHarnessStarts(t, h, 2)
	second := h.prompts()[1]
	if !strings.Contains(second, rejectCorrection) {
		t.Fatalf("second harness prompt missing correction:\n%s", second)
	}
	if !strings.Contains(second, "## Operator rejection") {
		t.Fatalf("second prompt missing rejection heading:\n%s", second)
	}

	foundReject := false
	for _, c := range tr.commentBodies() {
		if strings.Contains(c, "### Zeroth plan rejected") && strings.Contains(c, rejectCorrection) {
			foundReject = true
		}
	}
	if !foundReject {
		t.Fatalf("missing tracker rejection comment: %v", tr.commentBodies())
	}

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Unassigned, Key: "42-54", Issue: iss, At: time.Now()}
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-54", Issue: iss, At: time.Now()}
	waitHarnessStarts(t, h, 3)
	newest := waitNewestRunByTracker(t, hs.URL, "42-54", string(run.Id))
	if newest.Prompt == nil || !strings.Contains(*newest.Prompt, rejectCorrection) {
		t.Fatalf("new run prompt missing correction: %+v", newest.Prompt)
	}
	last := h.prompts()[len(h.prompts())-1]
	if !strings.Contains(last, rejectCorrection) {
		t.Fatalf("new-run harness prompt missing correction:\n%s", last)
	}
}

func TestRejectCommentFeedsNewRunThroughLinearDriver(t *testing.T) {
	t.Parallel()
	fake := linear.NewFake()
	gql := httptest.NewServer(fake)
	t.Cleanup(gql.Close)
	p, err := linear.New(linear.Config{
		APIKey:       fake.APIKey,
		Endpoint:     gql.URL,
		AgentUserID:  fake.AgentUserID,
		PollInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newRejectHarness()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:   st,
		Tracker: p,
		Sandbox: newFakeSandbox(),
		Harness: h,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	fake.SetAssignee("42-1", fake.AgentUserID)
	run := waitRunByTracker(t, hs.URL, "42-1")
	got := waitRunPlan(t, hs.URL, string(run.Id))
	waitHarnessStarts(t, h, 1)

	res := postJSON(t, hs.URL+"/plans/"+string(*got.PlanId)+"/request-changes", gen.RequestChangesRequest{
		Comment: rejectCorrection,
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("request-changes %d %s", res.StatusCode, slurp)
	}

	waitHarnessStarts(t, h, 2)
	if !strings.Contains(h.prompts()[1], rejectCorrection) {
		t.Fatalf("same-run prompt missing correction:\n%s", h.prompts()[1])
	}

	deadline := time.Now().Add(3 * time.Second)
	posted := false
	for time.Now().Before(deadline) {
		for _, c := range fake.Comments() {
			if strings.Contains(c.Body, "### Zeroth plan rejected") && strings.Contains(c.Body, rejectCorrection) {
				posted = true
			}
		}
		if posted {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !posted {
		t.Fatalf("Linear GraphQL never stored the rejection: %+v", fake.Comments())
	}

	listed, err := p.ListComments(t.Context(), "42-1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range listed {
		if strings.Contains(c.Body, rejectCorrection) {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListComments missing correction: %+v", listed)
	}

	fake.SetAssignee("42-1", "")
	waitRunStatus(t, hs.URL, string(run.Id), gen.RunStatusFailed)
	fake.SetAssignee("42-1", fake.AgentUserID)
	waitHarnessStarts(t, h, 3)
	newest := waitNewestRunByTracker(t, hs.URL, "42-1", string(run.Id))
	if newest.Prompt == nil || !strings.Contains(*newest.Prompt, rejectCorrection) {
		t.Fatalf("new Linear run prompt missing correction: %+v", newest.Prompt)
	}
	last := h.prompts()[len(h.prompts())-1]
	if !strings.Contains(last, rejectCorrection) {
		t.Fatalf("new Linear run harness prompt missing correction:\n%s", last)
	}
}

func newRejectHarness() *stubHarness {
	return &stubHarness{
		events: []harness.Event{
			{Kind: harness.EventToken, Payload: "drafting"},
			{Kind: harness.EventEffects, Effects: []harness.Effect{
				{Type: "create", Path: "README.md", Diff: "+## Documentation\n"},
			}},
			{Kind: harness.EventExited, Payload: "0"},
		},
	}
}

func waitNewestRunByTracker(t *testing.T, base, key, oldID string) gen.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
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
			if run.TrackerRef != nil && *run.TrackerRef == key && string(run.Id) != oldID {
				return run
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a new run with tracker_ref %s", key)
	return gen.Run{}
}
