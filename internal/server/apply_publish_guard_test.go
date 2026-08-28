package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/plan"
)

// 42-77: an approved plan that writes files but publishes nothing reported
// StatusApplied and moved its tracker issue to "In Review" with no PR. The
// silent step was publishApplied returning nil on empty targets. Not every
// empty publish is wrong, though: memory_proposal rows never touch the
// worktree, and compiled memory artifacts are deliberately kept out of git.

func TestRowsExpectingPublish(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rows []plan.Row
		want int
	}{
		{"no rows", nil, 0},
		{
			"file rows count",
			[]plan.Row{
				{Op: plan.OpCreate, Target: "CHANGELOG.md"},
				{Op: plan.OpModify, Target: "README.md"},
				{Op: plan.OpDestroy, Target: "old.txt"},
			},
			3,
		},
		{
			// These never reach the worktree, so publishing nothing is right.
			"memory proposals do not count",
			[]plan.Row{
				{Op: plan.OpMemoryProposal, Target: "fact-1"},
				{Op: plan.OpMemoryProposal, Target: "fact-2"},
			},
			0,
		},
		{
			// Compiled memory is a build artifact; skipGitTarget keeps it out.
			"compiled memory paths do not count",
			[]plan.Row{
				{Op: plan.OpModify, Target: "AGENTS.md"},
				{Op: plan.OpModify, Target: "CLAUDE.md"},
			},
			0,
		},
		{
			"mixed counts only the publishable file row",
			[]plan.Row{
				{Op: plan.OpMemoryProposal, Target: "fact-1"},
				{Op: plan.OpModify, Target: "AGENTS.md"},
				{Op: plan.OpCreate, Target: "docs/new.md"},
			},
			1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := rowsExpectingPublish(plan.Plan{Rows: tc.rows}); got != tc.want {
				t.Fatalf("rowsExpectingPublish = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPublishAppliedFailsWhenFileRowsPublishNothing(t *testing.T) {
	t.Parallel()
	// The exact shape of the live incident: a single create effect, apply
	// reports success, and nothing reaches git.
	s := &Server{}
	p := plan.Plan{Rows: []plan.Row{{Op: plan.OpCreate, Target: "CHANGELOG.md"}}}
	world := newApplyWorld(t.TempDir())

	err := s.publishAppliedTargets(p, world.targets())
	if err == nil {
		t.Fatal("apply that wrote no publishable file reported success")
	}
	if errors.Is(err, errNothingToPublish) {
		t.Fatalf("err = %v, want a real failure, not the legitimate-empty marker", err)
	}
	if !strings.Contains(err.Error(), "wrote nothing to publish") {
		t.Fatalf("err = %v, want it to say nothing was published", err)
	}
}

func TestPublishAppliedAllowsLegitimateEmptyPublish(t *testing.T) {
	t.Parallel()
	// A memory-proposal-only plan and a compiled-artifact-only plan both
	// publish nothing by design. Neither is a failure.
	s := &Server{}
	for _, tc := range []struct {
		name string
		rows []plan.Row
	}{
		{"memory proposals only", []plan.Row{{Op: plan.OpMemoryProposal, Target: "fact-1"}}},
		{"compiled artifacts only", []plan.Row{{Op: plan.OpModify, Target: "AGENTS.md"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := s.publishAppliedTargets(plan.Plan{Rows: tc.rows}, nil)
			if !errors.Is(err, errNothingToPublish) {
				t.Fatalf("err = %v, want the legitimate-empty marker", err)
			}
		})
	}
}
