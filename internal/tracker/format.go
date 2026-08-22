package tracker

import (
	"fmt"
	"strings"
)

// PlanExam is the cross-exam line operators read before opening the
// collapsed plan body. Empty Verdict omits the line.
type PlanExam struct {
	Verdict string
	Model   string
	Notes   string
}

// PlanComment is the tracker body for a drafted plan. The verdict stays
// visible. Diffs collapse.
type PlanComment struct {
	Hash    string
	Summary string
	Body    string
	Exam    PlanExam
}

// FormatPlanComment renders a plan for a tracker comment. The full plan
// body is collapsed so Linear (and a later GitHub Issues view) stays
// readable. The summary and cross-exam verdict stay visible: a fail
// must not be buried in details.
func FormatPlanComment(c PlanComment) string {
	var b strings.Builder
	b.WriteString("### Zeroth plan\n\n")
	if s := strings.TrimSpace(c.Summary); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	writeExam(&b, c.Exam)
	b.WriteString("<details>\n<summary>Plan")
	if h := strings.TrimSpace(c.Hash); h != "" {
		fmt.Fprintf(&b, " <code>%s</code>", h)
	}
	b.WriteString("</summary>\n\n")
	open, close := codeFence(c.Body)
	b.WriteString(open)
	b.WriteByte('\n')
	b.WriteString(c.Body)
	if c.Body != "" && !strings.HasSuffix(c.Body, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(close)
	b.WriteString("\n\n</details>\n")
	return b.String()
}

func writeExam(b *strings.Builder, exam PlanExam) {
	verdict := strings.TrimSpace(exam.Verdict)
	if verdict == "" {
		return
	}
	fmt.Fprintf(b, "**Cross-exam: %s**", verdict)
	if m := strings.TrimSpace(exam.Model); m != "" {
		fmt.Fprintf(b, " (`%s`)", m)
	}
	b.WriteByte('\n')
	if flagsConcern(verdict) {
		b.WriteString("\nReviewer flagged a concern. Read this before approving.\n")
	}
	if notes := strings.TrimSpace(exam.Notes); notes != "" {
		b.WriteByte('\n')
		for _, line := range strings.Split(notes, "\n") {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
}

func flagsConcern(verdict string) bool {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "fail", "pass_with_notes":
		return true
	default:
		return false
	}
}

// FormatStartedComment is posted when assign-to-Zeroth opens a run.
func FormatStartedComment(runID, issueKey string) string {
	var b strings.Builder
	b.WriteString("### Zeroth started\n\n")
	fmt.Fprintf(&b, "Assigned %s to the agent. Headless run `%s` is in the sandbox.\n\n", issueKey, runID)
	b.WriteString("A plan will be commented here before apply. Un-assign to cancel; that stops the sandbox, not just the row.\n")
	return b.String()
}

// FormatCompletion renders the close-out comment. Cost, transcript, PR,
// and audit are the fields someone reading only the tracker needs.
func FormatCompletion(c Completion) string {
	var b strings.Builder
	b.WriteString("### Zeroth completed\n\n")
	if c.RunID != "" {
		fmt.Fprintf(&b, "Run `%s` finished.\n\n", c.RunID)
	}
	b.WriteString("| | |\n| --- | --- |\n")
	fmt.Fprintf(&b, "| Cost | %s |\n", display(c.Cost, "not metered"))
	fmt.Fprintf(&b, "| Transcript | %s |\n", displayLink(c.Transcript))
	fmt.Fprintf(&b, "| Pull request | %s |\n", displayLink(c.PullRequest))
	fmt.Fprintf(&b, "| Audit | %s |\n", display(c.Audit, "see local event log"))
	return b.String()
}

// FormatCancelComment is posted when un-assign stops a run.
func FormatCancelComment(runID string) string {
	var b strings.Builder
	b.WriteString("### Zeroth cancelled\n\n")
	b.WriteString("Un-assigned mid-run. The sandbox has been stopped, not only marked cancelled.\n")
	if runID != "" {
		fmt.Fprintf(&b, "\nRun `%s` is failed.\n", runID)
	}
	return b.String()
}

// FormatFailedComment is posted when a run ends without applying, or
// when apply refuses (stale preconditions, postcondition mismatch).
func FormatFailedComment(runID, reason string) string {
	var b strings.Builder
	b.WriteString("### Zeroth failed\n\n")
	if r := strings.TrimSpace(reason); r != "" {
		b.WriteString(r)
		b.WriteString("\n\n")
	} else {
		b.WriteString("The run ended without a change plan.\n\n")
	}
	if runID != "" {
		fmt.Fprintf(&b, "Run `%s` is failed. See local daemon logs.\n", runID)
	}
	return b.String()
}

func codeFence(body string) (open, close string) {
	// Nested markdown fences break Linear's renderer. A longer tilde
	// fence stays valid when the plan body already contains ``` diffs.
	if strings.Contains(body, "```") {
		return "~~~~", "~~~~"
	}
	return "```", "```"
}

func display(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func displayLink(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "none"
	}
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		return fmt.Sprintf("[open](%s)", v)
	}
	return "`" + v + "`"
}
