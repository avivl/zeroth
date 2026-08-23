package server

import (
	"strings"
	"testing"
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
