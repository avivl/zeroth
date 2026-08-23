package server_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/tracker"
)

func TestAssignCarriesSettledDecisionIntoFreshRun(t *testing.T) {
	t.Parallel()
	decision := "The new doc should live at flat docs/linear-setup.md, not a new docs/operator/ subfolder. Existing docs/ folders (adr/, design/, prd/, spike/) are each a document type."
	first := tracker.Issue{
		Key:         "42-43",
		ID:          "iss_42_43",
		Title:       "README: document the assign-to-Zeroth setup steps",
		Description: "Add the operator setup walkthrough.",
	}
	second := tracker.Issue{
		Key:         "42-55",
		ID:          "iss_42_55",
		Title:       "Document the webhook secret setup",
		Description: "Add a docs page for the webhook secret. Follow existing docs conventions.",
	}
	tr := newStubTracker(first)
	tr.putIssue(second)
	tr.putThread("42-43", []tracker.Comment{
		{
			ID:     "cmt_decision",
			Body:   decision,
			Author: "alice",
			At:     time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC),
		},
		{
			ID:     "cmt_zeroth",
			Body:   tracker.FormatStartedComment("s_old", "42-43"),
			Author: "alice",
		},
		{
			ID:     "cmt_bot",
			Body:   "ignore this bot note about docs/operator/",
			Author: "cursor",
			Bot:    true,
		},
	})
	h := &stubHarness{
		events: []harness.Event{
			{Kind: harness.EventEffects, Effects: []harness.Effect{
				{Type: "create", Path: "docs/linear-setup.md", Diff: "+setup"},
			}},
			{Kind: harness.EventExited, Payload: "0"},
		},
	}
	sbx := newFakeSandbox()
	hs := harnessAssignServer(t, tr, sbx, h)

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-43", Issue: first, At: time.Now()}
	run1 := waitRunByTracker(t, hs.URL, "42-43")
	_ = waitRunPlan(t, hs.URL, string(run1.Id))
	if run1.Prompt == nil {
		t.Fatal("first run missing prompt")
	}
	if !strings.Contains(*run1.Prompt, decision) {
		t.Fatalf("first run prompt missing operator decision:\n%s", *run1.Prompt)
	}
	if !strings.Contains(h.lastPrompt(), decision) {
		t.Fatalf("harness prompt missing operator decision:\n%s", h.lastPrompt())
	}
	if !strings.Contains(*run1.Prompt, "## Comment thread") {
		t.Fatalf("first run prompt missing comment thread:\n%s", *run1.Prompt)
	}
	if strings.Contains(*run1.Prompt, "### Zeroth started") {
		t.Fatalf("first run prompt included Zeroth system comment:\n%s", *run1.Prompt)
	}
	if strings.Contains(*run1.Prompt, "ignore this bot note") {
		t.Fatalf("first run prompt included bot comment:\n%s", *run1.Prompt)
	}

	agents := joinOverlay(t, sbx, memory.CompiledAgents)
	if !strings.Contains(agents, decision) {
		t.Fatalf("first run AGENTS.md missing ingested decision:\n%s", agents)
	}
	if strings.Contains(agents, "ignore this bot note") {
		t.Fatalf("bot comment leaked into AGENTS.md:\n%s", agents)
	}

	tr.ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-55", Issue: second, At: time.Now()}
	run2 := waitRunByTracker(t, hs.URL, "42-55")
	_ = waitRunPlan(t, hs.URL, string(run2.Id))
	if run2.Prompt == nil {
		t.Fatal("fresh run missing prompt")
	}
	if !strings.Contains(*run2.Prompt, decision) {
		t.Fatalf("fresh run prompt missing settled memory:\n%s", *run2.Prompt)
	}
	if !strings.Contains(h.lastPrompt(), decision) {
		t.Fatalf("fresh harness prompt missing settled memory:\n%s", h.lastPrompt())
	}
	if !strings.Contains(*run2.Prompt, "## Project memory") {
		t.Fatalf("fresh run prompt missing project memory section:\n%s", *run2.Prompt)
	}
	if strings.Contains(*run2.Prompt, "## Comment thread") {
		t.Fatalf("fresh run should not invent a comment thread:\n%s", *run2.Prompt)
	}

	found := false
	for _, body := range sbx.overlayFiles(memory.CompiledAgents) {
		if strings.Contains(body, decision) {
			found = true
		}
	}
	if !found {
		t.Fatalf("fresh run overlay missing settled decision in AGENTS.md: %v", sbx.overlayFiles(memory.CompiledAgents))
	}
	if h.startCount() != 2 {
		t.Fatalf("harness starts = %d, want 2", h.startCount())
	}
}

func joinOverlay(t *testing.T, sbx *fakeSandbox, rel string) string {
	t.Helper()
	bodies := sbx.overlayFiles(rel)
	if len(bodies) == 0 {
		// Sandbox may have been listed before hydrate landed. Read any remaining host dir.
		id := sbx.anyID()
		if id.IsZero() {
			t.Fatal("no sandbox overlay")
		}
		dir, err := sbx.HostWorkspace(id)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(raw)
	}
	return strings.Join(bodies, "\n---\n")
}
