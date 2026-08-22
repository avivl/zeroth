package plan

import (
	"fmt"
	"strings"

	"github.com/avivl/zeroth/internal/policy"
)

// Issue is the tracker issue the reviewer is allowed to see. It is not
// the producer's transcript.
type Issue struct {
	Ref   string
	Title string
	Body  string
}

// Diff is one row as the reviewer sees it: operation, target, payload.
// Lease ids, hashes, and idempotency keys stay out. They are apply
// machinery, not something a second model needs to rubber-stamp.
type Diff struct {
	Op      Op
	Target  string
	Payload string
}

// Packet is the independent review context: issue + plan + diffs.
// There is no transcript field. Callers that have producer tokens must
// not pass them in through Issue either; PacketFrom never reads a log.
type Packet struct {
	Issue   Issue
	Summary string
	Hash    policy.PlanHash
	Scope   policy.ScopeID
	Diffs   []Diff
}

// PacketFrom builds the reviewer context from a draft and its issue.
// The producer's chain of thought is not an argument and cannot leak
// in through this constructor.
func PacketFrom(p Plan, issue Issue) Packet {
	diffs := make([]Diff, 0, len(p.Rows))
	for _, r := range p.Rows {
		diffs = append(diffs, Diff{Op: r.Op, Target: r.Target, Payload: r.Payload})
	}
	return Packet{
		Issue:   issue,
		Summary: p.Summary,
		Hash:    p.Hash,
		Scope:   p.Scope,
		Diffs:   diffs,
	}
}

// Encode is the only byte sequence a reviewer is given. It is issue,
// plan constraints, and diffs. It is not a session log.
func (p Packet) Encode() string {
	var b strings.Builder
	b.WriteString("You are an independent reviewer of this plan. You have not seen the producer's reasoning. Do not assume it was careful.\n")
	b.WriteString("Return a verdict of pass, fail, or pass_with_notes, plus notes.\n")
	b.WriteString("Catch scope violations: work outside the issue, extra paths, secrets, or grants the issue did not ask for.\n\n")
	b.WriteString("# Issue\n")
	if p.Issue.Ref != "" {
		fmt.Fprintf(&b, "Ref: %s\n", p.Issue.Ref)
	}
	if p.Issue.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", p.Issue.Title)
	}
	body := strings.TrimSpace(p.Issue.Body)
	if body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	b.WriteString("\n# Plan\n")
	fmt.Fprintf(&b, "Summary: %s\n", p.Summary)
	fmt.Fprintf(&b, "Hash: %s\n", p.Hash)
	fmt.Fprintf(&b, "Scope: %s\n", p.Scope)
	b.WriteString("\n# Diffs\n")
	if len(p.Diffs) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for _, d := range p.Diffs {
		fmt.Fprintf(&b, "## %s %s\n", d.Op, d.Target)
		b.WriteString(d.Payload)
		if !strings.HasSuffix(d.Payload, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
