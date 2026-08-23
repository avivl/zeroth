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

func TestFormatRetractComment(t *testing.T) {
	t.Parallel()
	body := tracker.FormatRetractComment(tracker.Retract{
		RunID:       "s_48",
		Reason:      "Apply overwrote README.md instead of patching it.",
		PullRequest: "https://github.com/avivl/zeroth/pull/48",
		Closed:      true,
	})
	for _, want := range []string{
		"### Zeroth retracted",
		"`s_48`",
		"Apply overwrote README.md instead of patching it.",
		"[open](https://github.com/avivl/zeroth/pull/48)",
		"(closed)",
		"fresh assignment",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestFormatRetractCommentWithoutPR(t *testing.T) {
	t.Parallel()
	body := tracker.FormatRetractComment(tracker.Retract{Reason: "unsafe apply"})
	for _, want := range []string{
		"### Zeroth retracted",
		"Prior run output has been retracted.",
		"unsafe apply",
		"none opened",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	fallback := tracker.FormatRetractComment(tracker.Retract{Reason: "  "})
	if !strings.Contains(fallback, "Prior run output has been retracted") {
		t.Fatalf("missing fallback: %s", fallback)
	}
	if !strings.Contains(fallback, "none opened") {
		t.Fatalf("missing none: %s", fallback)
	}
	open := tracker.FormatRetractComment(tracker.Retract{
		PullRequest: "https://github.com/avivl/zeroth/pull/1",
	})
	if strings.Contains(open, "(closed)") {
		t.Fatalf("open pr should not say closed: %s", open)
	}
	if !strings.Contains(open, "[open](https://github.com/avivl/zeroth/pull/1)") {
		t.Fatalf("open pr link: %s", open)
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

func TestIsSystemComment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		body string
		want bool
	}{
		{tracker.FormatStartedComment("s_1", "42-43"), true},
		{tracker.FormatPlanComment(tracker.PlanComment{Hash: "h", Summary: "touch README", Body: "diff"}), true},
		{tracker.FormatCompletion(tracker.Completion{RunID: "s_1"}), true},
		{tracker.FormatCancelComment("s_1"), true},
		{tracker.FormatFailedComment("s_1", "no plan"), true},
		{"The new doc should live at docs/linear-setup.md, not docs/operator/.", false},
		{"### not zeroth", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := tracker.IsSystemComment(tc.body); got != tc.want {
			t.Fatalf("IsSystemComment(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestHumanCommentsDropsSystemAndBots(t *testing.T) {
	t.Parallel()
	in := []tracker.Comment{
		{ID: "1", Body: "put it at docs/linear-setup.md", Author: "alice"},
		{ID: "2", Body: tracker.FormatStartedComment("s_1", "42-43"), Author: "alice"},
		{ID: "3", Body: "bot chatter", Author: "zeroth", Bot: true},
		{ID: "4", Body: "   ", Author: "alice"},
		{ID: "5", Body: "existing docs/ folders are document types", Author: "alice"},
	}
	got := tracker.HumanComments(in)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].ID != "1" || got[1].ID != "5" {
		t.Fatalf("order %+v", got)
	}
}
