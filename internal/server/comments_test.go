package server

import (
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/tracker"
)

func TestIssuePromptIncludesHumanCommentsAndMemory(t *testing.T) {
	t.Parallel()
	decision := "put the file at docs/linear-setup.md"
	iss := tracker.Issue{Key: "42-43", Title: "document setup", Description: "write the walkthrough"}
	comments := []tracker.Comment{
		{Body: decision, Author: "alice", At: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)},
		{Body: tracker.FormatFailedComment("s_1", "no plan"), Author: "alice"},
	}
	facts := []memory.Fact{{
		Kind: memory.KindOperator,
		Key:  "comment.42-1",
		Body: "prefer table tests",
	}}
	got := issuePrompt("42-43", iss, comments, facts)
	if !strings.Contains(got, "Linear 42-43: document setup") {
		t.Fatalf("title: %s", got)
	}
	if !strings.Contains(got, "write the walkthrough") {
		t.Fatalf("description: %s", got)
	}
	if !strings.Contains(got, decision) || !strings.Contains(got, "## Comment thread") {
		t.Fatalf("comment thread: %s", got)
	}
	if strings.Contains(got, "### Zeroth failed") {
		t.Fatalf("system comment leaked: %s", got)
	}
	if !strings.Contains(got, "## Project memory") || !strings.Contains(got, "prefer table tests") {
		t.Fatalf("project memory: %s", got)
	}
}

func TestCommentFactBodyJoinsOperatorThread(t *testing.T) {
	t.Parallel()
	body := commentFactBody("42-43", []tracker.Comment{
		{Author: "alice", Body: "use docs/linear-setup.md"},
		{Author: "", Body: "not docs/operator/"},
	})
	if !strings.Contains(body, "Linear 42-43") {
		t.Fatalf("issue key: %s", body)
	}
	if !strings.Contains(body, "[alice]") || !strings.Contains(body, "[operator]") {
		t.Fatalf("authors: %s", body)
	}
	if !strings.Contains(body, "use docs/linear-setup.md") || !strings.Contains(body, "not docs/operator/") {
		t.Fatalf("bodies: %s", body)
	}
}
