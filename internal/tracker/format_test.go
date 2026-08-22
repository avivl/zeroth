package tracker_test

import (
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/tracker"
)

func TestFormatPlanCommentCollapsesDiff(t *testing.T) {
	t.Parallel()
	body := tracker.FormatPlanComment(tracker.PlanComment{
		Hash:    "abc123",
		Summary: "touch README",
		Body:    "```diff\n-a\n+b\n```",
	})
	if !strings.Contains(body, "### Zeroth plan") {
		t.Fatalf("missing heading: %s", body)
	}
	if !strings.Contains(body, "touch README") {
		t.Fatalf("summary should stay visible: %s", body)
	}
	if !strings.Contains(body, "<details>") || !strings.Contains(body, "</details>") {
		t.Fatalf("plan body must collapse: %s", body)
	}
	if !strings.Contains(body, "<code>abc123</code>") {
		t.Fatalf("hash should be in the summary: %s", body)
	}
	if !strings.Contains(body, "~~~~") {
		t.Fatalf("nested ``` must use a tilde fence: %s", body)
	}
	if strings.Contains(body, "```\n```diff") {
		t.Fatalf("backtick fence around a diff breaks Linear: %s", body)
	}
}

func TestFormatPlanCommentShowsExamOutsideDetails(t *testing.T) {
	t.Parallel()
	body := tracker.FormatPlanComment(tracker.PlanComment{
		Hash:    "abc123",
		Summary: "touch README",
		Body:    "```diff\n-a\n+b\n```",
		Exam: tracker.PlanExam{
			Verdict: "fail",
			Model:   "gpt-4o",
			Notes:   "scope violation: .ssh/authorized_keys",
		},
	})
	visible, _, ok := strings.Cut(body, "<details>")
	if !ok {
		t.Fatalf("missing details: %s", body)
	}
	for _, want := range []string{
		"**Cross-exam: fail**",
		"`gpt-4o`",
		"Reviewer flagged a concern",
		"scope violation: .ssh/authorized_keys",
	} {
		if !strings.Contains(visible, want) {
			t.Fatalf("visible prefix missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "```diff") {
		t.Fatalf("diff leaked outside details: %s", visible)
	}
}

func TestFormatCompletionTable(t *testing.T) {
	t.Parallel()
	body := tracker.FormatCompletion(tracker.Completion{
		RunID:       "s_1",
		Cost:        "0.12 USD",
		Transcript:  "zeroth attach s_1",
		PullRequest: "https://github.com/avivl/zeroth/pull/1",
		Audit:       "12 events, terminal=done",
	})
	for _, want := range []string{
		"### Zeroth completed",
		"`s_1`",
		"0.12 USD",
		"`zeroth attach s_1`",
		"[open](https://github.com/avivl/zeroth/pull/1)",
		"12 events, terminal=done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestFormatCompletionEmptyFields(t *testing.T) {
	t.Parallel()
	body := tracker.FormatCompletion(tracker.Completion{RunID: "s_2"})
	if !strings.Contains(body, "not metered") {
		t.Fatalf("cost fallback: %s", body)
	}
	if !strings.Contains(body, "none") {
		t.Fatalf("pr fallback: %s", body)
	}
}

func TestFormatStartedAndCancel(t *testing.T) {
	t.Parallel()
	start := tracker.FormatStartedComment("s_9", "42-29")
	if !strings.Contains(start, "42-29") || !strings.Contains(start, "`s_9`") {
		t.Fatalf("started: %s", start)
	}
	cancel := tracker.FormatCancelComment("s_9")
	if !strings.Contains(cancel, "sandbox has been stopped") {
		t.Fatalf("cancel must say the sandbox stopped: %s", cancel)
	}
}

func TestFormatFailedComment(t *testing.T) {
	t.Parallel()
	body := tracker.FormatFailedComment("s_9", "harness exited without proposing a plan")
	for _, want := range []string{
		"### Zeroth failed",
		"harness exited without proposing a plan",
		"`s_9`",
		"See local daemon logs",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}
