package tracker

import (
	"fmt"
	"strings"
)

// FormatPlanComment renders a plan for a tracker comment. The full plan
// body is collapsed so Linear (and a later GitHub Issues view) stays
// readable; the summary stays visible.
func FormatPlanComment(hash, summary, rendered string) string {
	var b strings.Builder
	b.WriteString("### Zeroth plan\n\n")
	if s := strings.TrimSpace(summary); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	b.WriteString("<details>\n<summary>Plan")
	if h := strings.TrimSpace(hash); h != "" {
		fmt.Fprintf(&b, " <code>%s</code>", h)
	}
	b.WriteString("</summary>\n\n")
	open, close := codeFence(rendered)
	b.WriteString(open)
	b.WriteByte('\n')
	b.WriteString(rendered)
	if rendered != "" && !strings.HasSuffix(rendered, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(close)
	b.WriteString("\n\n</details>\n")
	return b.String()
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

// FormatRejectedComment is posted when the operator rejects a draft
// with feedback. The next run reads this thread, so the correction is
// in the plan-drafting prompt rather than only on the previous plan.
func FormatRejectedComment(runID, planID, comment string) string {
	var b strings.Builder
	b.WriteString("### Zeroth plan rejected\n\n")
	if c := strings.TrimSpace(comment); c != "" {
		b.WriteString(c)
		b.WriteString("\n\n")
	}
	b.WriteString("The next plan must address this feedback. Un-assign is not required.\n")
	if runID != "" || planID != "" {
		b.WriteByte('\n')
		if runID != "" {
			fmt.Fprintf(&b, "Run `%s`. ", runID)
		}
		if planID != "" {
			fmt.Fprintf(&b, "Plan `%s`.", planID)
		}
		b.WriteByte('\n')
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
