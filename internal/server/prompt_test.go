package server

import (
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/tracker"
)

func TestAppendOperatorRejection(t *testing.T) {
	t.Parallel()
	got := appendOperatorRejection("Linear 42-54: docs", "that heading doesn't exist, use the real one")
	if !strings.Contains(got, "Linear 42-54: docs") {
		t.Fatalf("lost issue body: %s", got)
	}
	if !strings.Contains(got, operatorRejectionHeading) {
		t.Fatalf("missing heading: %s", got)
	}
	if !strings.Contains(got, "that heading doesn't exist, use the real one") {
		t.Fatalf("missing correction: %s", got)
	}
	if got := appendOperatorRejection("keep", "  "); got != "keep" {
		t.Fatalf("empty comment rewrote prompt: %q", got)
	}
}

func TestAppendIssueCommentsOldestFirst(t *testing.T) {
	t.Parallel()
	got := appendIssueComments("Linear 42-1: title\n\nbody", []tracker.IssueComment{
		{Body: "earlier note", Author: "alice", CreatedAt: time.Unix(1, 0)},
		{Body: "that heading doesn't exist, use the real one", Author: "bob", CreatedAt: time.Unix(2, 0)},
	})
	if !strings.Contains(got, "## Issue comments") {
		t.Fatalf("missing comments section: %s", got)
	}
	ai := strings.Index(got, "earlier note")
	bi := strings.Index(got, "that heading doesn't exist, use the real one")
	if ai < 0 || bi < 0 || bi < ai {
		t.Fatalf("order: %s", got)
	}
	if !strings.Contains(got, "### bob") {
		t.Fatalf("missing author: %s", got)
	}
	if got := appendIssueComments("keep", nil); got != "keep" {
		t.Fatalf("empty comments rewrote prompt: %q", got)
	}
}
