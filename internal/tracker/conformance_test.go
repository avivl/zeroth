package tracker_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/tracker"
	"github.com/avivl/zeroth/internal/tracker/linear"
)

type harness struct {
	name string
	open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)
}

func TestProviderConformance(t *testing.T) {
	t.Parallel()

	// Adding a provider is one more row. Cases stay implementation-agnostic
	// except for the open hook that points Linear at a GraphQL double (NFR-4, Z1-071).
	cases := []harness{
		{
			name: "linear",
			open: func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL) {
				t.Helper()
				fake := linear.NewFake()
				srv := httptest.NewServer(fake)
				t.Cleanup(srv.Close)
				p, err := linear.New(linear.Config{
					APIKey:       fake.APIKey,
					Endpoint:     srv.URL,
					AgentUserID:  fake.AgentUserID,
					PollInterval: 20 * time.Millisecond,
				})
				if err != nil {
					t.Fatalf("linear.New: %v", err)
				}
				return p, fake
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, _ := tc.open(t)
			if p == nil {
				t.Fatal("nil provider")
			}
			if got := p.Name(); got != tc.name {
				t.Fatalf("Name() = %q, want %q", got, tc.name)
			}
			_ = p.Capabilities()

			t.Run("get_issue", func(t *testing.T) { testGetIssue(t, tc.open) })
			t.Run("get_issue_missing", func(t *testing.T) { testGetIssueMissing(t, tc.open) })
			t.Run("get_issue_empty", func(t *testing.T) { testGetIssueEmpty(t, tc.open) })
			t.Run("comment", func(t *testing.T) { testComment(t, tc.open) })
			t.Run("comment_invalid", func(t *testing.T) { testCommentInvalid(t, tc.open) })
			t.Run("set_state", func(t *testing.T) { testSetState(t, tc.open) })
			t.Run("link_artifact", func(t *testing.T) { testLinkArtifact(t, tc.open) })
			t.Run("unassign", func(t *testing.T) { testUnassign(t, tc.open) })
			t.Run("unassign_ready_for_reassign", func(t *testing.T) { testUnassignReadyForReassign(t, tc.open) })
			t.Run("assignments_assign_unassign", func(t *testing.T) { testAssignments(t, tc.open) })
		})
	}
}

func testGetIssue(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, _ := open(t)
	iss, err := p.GetIssue(t.Context(), "42-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if iss.Key != "42-1" {
		t.Fatalf("key = %q", iss.Key)
	}
	if iss.Title == "" {
		t.Fatal("empty title")
	}
}

func testGetIssueMissing(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, _ := open(t)
	_, err := p.GetIssue(t.Context(), "no-such-issue")
	if !errors.Is(err, tracker.ErrNotFound) {
		t.Fatalf("GetIssue missing = %v, want ErrNotFound", err)
	}
}

func testGetIssueEmpty(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, _ := open(t)
	_, err := p.GetIssue(t.Context(), "  ")
	if !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("GetIssue empty = %v, want ErrInvalid", err)
	}
}

func testComment(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, fake := open(t)
	plan := tracker.FormatPlanComment("hash1", "touch README", "```diff\n-a\n+b\n```")
	ref, err := p.Comment(t.Context(), "42-1", plan)
	if err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if ref.ID == "" {
		t.Fatal("empty comment id")
	}
	found := false
	for _, c := range fake.Comments() {
		if c.ID == ref.ID {
			found = true
			if !strings.Contains(c.Body, "<details>") {
				t.Fatalf("plan comment not collapsed: %s", c.Body)
			}
			if !strings.Contains(c.Body, "~~~~") {
				t.Fatalf("plan diff fence: %s", c.Body)
			}
		}
	}
	if !found {
		t.Fatal("comment not stored on fake")
	}
}

func testCommentInvalid(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, _ := open(t)
	if _, err := p.Comment(t.Context(), "42-1", ""); !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("empty body = %v", err)
	}
	if _, err := p.Comment(t.Context(), "", "hi"); !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("empty key = %v", err)
	}
}

func testSetState(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, fake := open(t)
	if err := p.SetState(t.Context(), "42-1", tracker.State{Kind: tracker.StateStarted}); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if got := fake.IssueState("42-1"); got != string(tracker.StateStarted) {
		t.Fatalf("state = %q, want started", got)
	}
	if err := p.SetState(t.Context(), "42-1", tracker.State{}); !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("empty kind = %v", err)
	}
}

func testLinkArtifact(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, fake := open(t)
	a := tracker.Artifact{
		Kind:  tracker.ArtifactPR,
		URL:   "https://github.com/avivl/zeroth/pull/1",
		Title: "PR",
	}
	if err := p.LinkArtifact(t.Context(), "42-1", a); err != nil {
		t.Fatalf("LinkArtifact: %v", err)
	}
	atts := fake.Attachments()
	if len(atts) != 1 || atts[0].URL != a.URL {
		t.Fatalf("attachments = %+v", atts)
	}
	if err := p.LinkArtifact(t.Context(), "42-1", tracker.Artifact{Kind: tracker.ArtifactPR}); !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("empty url = %v", err)
	}
}

func testUnassign(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, fake := open(t)
	if err := p.Unassign(t.Context(), "  "); !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("empty key = %v, want ErrInvalid", err)
	}
	if err := p.Unassign(t.Context(), "no-such-issue"); !errors.Is(err, tracker.ErrNotFound) {
		t.Fatalf("missing = %v, want ErrNotFound", err)
	}
	if err := p.Unassign(t.Context(), "42-1"); err != nil {
		t.Fatalf("already clear: %v", err)
	}
	fake.SetAssignee("42-1", fake.AgentUserID)
	fake.SetDelegate("42-1", fake.AgentUserID)
	if err := p.Unassign(t.Context(), "42-1"); err != nil {
		t.Fatalf("Unassign: %v", err)
	}
	iss, err := p.GetIssue(t.Context(), "42-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if iss.AssigneeID != "" || iss.DelegateID != "" {
		t.Fatalf("still claimed: assignee=%q delegate=%q", iss.AssigneeID, iss.DelegateID)
	}
}

func testUnassignReadyForReassign(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, fake := open(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatalf("Assignments: %v", err)
	}
	fake.SetAssignee("42-1", fake.AgentUserID)
	fake.SetDelegate("42-1", fake.AgentUserID)
	_ = waitAssignment(t, ch, tracker.Assigned, "42-1")
	if err := p.Unassign(t.Context(), "42-1"); err != nil {
		t.Fatalf("Unassign: %v", err)
	}
	select {
	case ev := <-ch:
		t.Fatalf("Unassign emitted %s %s (retract must not look like a mid-run cancel)", ev.Kind, ev.Key)
	case <-time.After(150 * time.Millisecond):
	}
	fake.SetAssignee("42-1", fake.AgentUserID)
	_ = waitAssignment(t, ch, tracker.Assigned, "42-1")
}

func testAssignments(t *testing.T, open func(t *testing.T) (tracker.Provider, *linear.FakeGraphQL)) {
	t.Helper()
	p, fake := open(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatalf("Assignments: %v", err)
	}
	fake.SetAssignee("42-1", fake.AgentUserID)
	ev := waitAssignment(t, ch, tracker.Assigned, "42-1")
	if ev.Issue.Title == "" {
		t.Fatal("assigned event missing issue")
	}
	fake.SetAssignee("42-1", "")
	_ = waitAssignment(t, ch, tracker.Unassigned, "42-1")
	cancel()
	waitClosed(t, ch)
}

func waitAssignment(t *testing.T, ch <-chan tracker.AssignmentEvent, kind tracker.AssignmentKind, key string) tracker.AssignmentEvent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed waiting for %s %s", kind, key)
			}
			if ev.Kind == kind && ev.Key == key {
				return ev
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %s %s", kind, key)
		}
	}
}

func waitClosed(t *testing.T, ch <-chan tracker.AssignmentEvent) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			// drain until close or timeout
			deadline := time.After(2 * time.Second)
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
				case <-deadline:
					t.Fatal("assignments channel did not close")
				}
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("assignments channel did not close")
	}
}
