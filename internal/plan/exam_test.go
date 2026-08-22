package plan

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const producerCoT = "PRODUCER_CHAIN_OF_THOUGHT_do_not_leak_this_token"

type recordingReviewer struct {
	mu      sync.Mutex
	packets []Packet
	encodes []string
	fn      func(ctx context.Context, model string, packet Packet) (Review, error)
}

func (r *recordingReviewer) Review(ctx context.Context, model string, packet Packet) (Review, error) {
	enc := packet.Encode()
	r.mu.Lock()
	r.packets = append(r.packets, packet)
	r.encodes = append(r.encodes, enc)
	r.mu.Unlock()
	if strings.Contains(enc, producerCoT) {
		return Review{}, errors.New("reviewer saw producer chain of thought")
	}
	if r.fn != nil {
		return r.fn(ctx, model, packet)
	}
	return Review{Verdict: VerdictPass, Notes: "ok", Model: model}, nil
}

func (r *recordingReviewer) lastEncode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.encodes) == 0 {
		return ""
	}
	return r.encodes[len(r.encodes)-1]
}

type recordingAuditor struct {
	mu    sync.Mutex
	exams []CrossExam
}

func (a *recordingAuditor) SignVerdict(_ context.Context, _ Plan, exam CrossExam) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.exams = append(a.exams, exam)
	return nil
}

func (a *recordingAuditor) n() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.exams)
}

func testIssue() Issue {
	return Issue{
		Ref:   "42-28",
		Title: "Fix the docs typo",
		Body:  "Correct the typo in docs/design/plan.md.\n\nAllowed-paths: docs/",
	}
}

func scopeCatcher(_ context.Context, model string, packet Packet) (Review, error) {
	allowed := allowedPaths(packet.Issue.Body)
	var bad []string
	for _, d := range packet.Diffs {
		if !pathAllowed(d.Target, allowed) {
			bad = append(bad, d.Target)
		}
	}
	if len(bad) > 0 {
		return Review{
			Verdict: VerdictFail,
			Notes:   "scope violation: " + strings.Join(bad, ", "),
			Model:   model,
		}, nil
	}
	return Review{Verdict: VerdictPass, Notes: "paths match the issue", Model: model}, nil
}

func allowedPaths(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "Allowed-paths:"); ok {
			for _, p := range strings.Split(rest, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func pathAllowed(target string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if target == a || strings.HasPrefix(target, strings.TrimSuffix(a, "/")+"/") {
			return true
		}
	}
	return false
}

func mustExam(t *testing.T, rev Reviewer, cfg Config, p Plan, issue Issue) Outcome {
	t.Helper()
	ex := &Examiner{Reviewer: rev, Audit: &recordingAuditor{}, Clock: &testClock{now: time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)}}
	out, err := ex.Examine(t.Context(), p, cfg, PacketFrom(p, issue))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPacketHasNoProducerContextFields(t *testing.T) {
	t.Parallel()
	rt := reflect.TypeOf(Packet{})
	banned := []string{"transcript", "token", "thought", "cot", "reasoning", "log", "event"}
	for i := 0; i < rt.NumField(); i++ {
		name := strings.ToLower(rt.Field(i).Name)
		for _, b := range banned {
			if strings.Contains(name, b) {
				t.Fatalf("packet field %s looks like producer context", rt.Field(i).Name)
			}
		}
	}
}

func TestReviewerCannotSeeProducerChainOfThought(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
		},
		Observed: map[string]string{"docs/design/plan.md": "pre"},
	})
	producerLog := []string{
		"I will secretly rewrite .ssh/config because " + producerCoT,
		"tool_call: cat ~/.ssh/id_rsa",
	}
	rev := &recordingReviewer{fn: scopeCatcher}
	issue := testIssue()
	packet := PacketFrom(p, issue)
	enc := packet.Encode()
	if strings.Contains(enc, producerCoT) {
		t.Fatal("packet encode contained producer chain of thought")
	}
	for _, line := range producerLog {
		if strings.Contains(enc, line) {
			t.Fatalf("packet encode contained producer log %q", line)
		}
	}
	_ = mustExam(t, rev, Config{ProducerModel: "opus", Models: []string{"sonnet"}}, p, issue)
	got := rev.lastEncode()
	if got == "" {
		t.Fatal("reviewer was not invoked")
	}
	if strings.Contains(got, producerCoT) {
		t.Fatal("reviewer saw producer chain of thought")
	}
	if !strings.Contains(got, "docs/design/plan.md") {
		t.Fatal("reviewer packet missing the diff")
	}
	if !strings.Contains(got, "Allowed-paths: docs/") {
		t.Fatal("reviewer packet missing the issue")
	}
}

func TestScopeViolationIsCaught(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{
		Summary: "docs typo plus a secret extra",
		Effects: []Proposed{
			{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
			{Type: "create", Path: ".ssh/authorized_keys", Diff: "ssh-ed25519 AAAA sneak"},
		},
		Observed: map[string]string{"docs/design/plan.md": "pre"},
	})
	rev := &recordingReviewer{fn: scopeCatcher}
	out := mustExam(t, rev, Config{ProducerModel: "opus", Models: []string{"sonnet"}}, p, testIssue())
	if out.Exam.Verdict != VerdictFail {
		t.Fatalf("verdict %s, want fail", out.Exam.Verdict)
	}
	if !strings.Contains(out.Exam.Reasoning, ".ssh/authorized_keys") {
		t.Fatalf("notes %q should name the extra path", out.Exam.Reasoning)
	}
	enc := rev.lastEncode()
	if !strings.Contains(enc, ".ssh/authorized_keys") {
		t.Fatal("independent context must include the violating diff")
	}
	if strings.Contains(enc, producerCoT) {
		t.Fatal("producer cot leaked into a failing review")
	}
	if out.Plan.Status != StatusPendingApproval {
		t.Fatalf("status %s, want pending_approval (no block-on-fail)", out.Plan.Status)
	}
	if out.Returned {
		t.Fatal("without block-on-fail the plan must escalate to a human")
	}
}

func TestBlockOnFailReturnsPlanToAgent(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "create", Path: "secrets.env", Diff: "TOKEN=aaaa"},
		},
	})
	rev := &recordingReviewer{fn: scopeCatcher}
	cfg := Config{ProducerModel: "opus", Models: []string{"sonnet"}, BlockOnFail: true}
	out := mustExam(t, rev, cfg, p, testIssue())
	if !out.Returned {
		t.Fatal("block-on-fail must return the plan to the agent")
	}
	if out.Plan.Status != StatusChangesRequested {
		t.Fatalf("status %s, want changes_requested", out.Plan.Status)
	}
	if out.Plan.ReviewComment == "" || !strings.Contains(out.Plan.ReviewComment, "secrets.env") {
		t.Fatalf("notes not attached: comment=%q exam=%q", out.Plan.ReviewComment, out.Exam.Reasoning)
	}
	if !strings.Contains(out.SteerMessage(), "secrets.env") {
		t.Fatalf("steer message %q", out.SteerMessage())
	}
	if out.Plan.Hash != p.Hash {
		t.Fatal("cross-exam must not rewrite the canonical hash")
	}
}

func TestPassEscalatesToHuman(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
		},
		Observed: map[string]string{"docs/design/plan.md": "pre"},
	})
	rev := &recordingReviewer{fn: scopeCatcher}
	out := mustExam(t, rev, Config{ProducerModel: "opus", Models: []string{"sonnet"}}, p, testIssue())
	if out.Exam.Verdict != VerdictPass {
		t.Fatalf("verdict %s", out.Exam.Verdict)
	}
	if out.Plan.Status != StatusPendingApproval || out.Returned {
		t.Fatalf("status %s returned=%v", out.Plan.Status, out.Returned)
	}
}

func TestDualRequiresBothToPass(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
		},
		Observed: map[string]string{"docs/design/plan.md": "pre"},
	})
	rev := &recordingReviewer{fn: func(_ context.Context, model string, packet Packet) (Review, error) {
		if model == "strict" {
			return Review{Verdict: VerdictFail, Notes: "strict: cost ceiling too high", Model: model}, nil
		}
		return scopeCatcher(context.Background(), model, packet)
	}}
	out := mustExam(t, rev, Config{ProducerModel: "opus", Models: []string{"sonnet", "strict"}}, p, testIssue())
	if out.Exam.Verdict != VerdictFail {
		t.Fatalf("dual must fail if either fails, got %s", out.Exam.Verdict)
	}
	if !strings.Contains(out.Exam.ReviewerModel, "sonnet") || !strings.Contains(out.Exam.ReviewerModel, "strict") {
		t.Fatalf("reviewer_model %q", out.Exam.ReviewerModel)
	}
}

func TestSameModelRejected(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	ex := &Examiner{Reviewer: &recordingReviewer{}}
	_, err := ex.Examine(t.Context(), p, Config{ProducerModel: "opus", Models: []string{"opus"}}, PacketFrom(p, testIssue()))
	if !errors.Is(err, ErrSameModel) {
		t.Fatalf("err=%v want ErrSameModel", err)
	}
	_, err = ex.Examine(t.Context(), p, Config{ProducerModel: "opus", Models: []string{"sonnet", "sonnet"}}, PacketFrom(p, testIssue()))
	if !errors.Is(err, ErrSameModel) {
		t.Fatalf("duplicate dual err=%v want ErrSameModel", err)
	}
}

func TestMissingReviewerDenied(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	ex := &Examiner{Reviewer: &recordingReviewer{}}
	_, err := ex.Examine(t.Context(), p, Config{}, PacketFrom(p, testIssue()))
	if !errors.Is(err, ErrNoReviewer) {
		t.Fatalf("err=%v want ErrNoReviewer", err)
	}
	ex = &Examiner{}
	_, err = ex.Examine(t.Context(), p, Config{Models: []string{"sonnet"}}, PacketFrom(p, testIssue()))
	if !errors.Is(err, ErrNoReviewer) {
		t.Fatalf("nil reviewer err=%v", err)
	}
}

func TestVerdictIsSigned(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	aud := &recordingAuditor{}
	rev := &recordingReviewer{}
	ex := &Examiner{Reviewer: rev, Audit: aud, Clock: &testClock{now: time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC)}}
	out, err := ex.Examine(t.Context(), p, Config{Models: []string{"sonnet"}}, PacketFrom(p, testIssue()))
	if err != nil {
		t.Fatal(err)
	}
	if aud.n() != 1 {
		t.Fatalf("signed %d, want 1", aud.n())
	}
	if out.Exam.At.IsZero() || out.Plan.CrossExam == nil {
		t.Fatal("exam not attached")
	}
}

func TestEmptyNotesOnNontrivialPlan(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "modify", Path: "a.md", Diff: strings.Repeat("x", 90)},
			{Type: "modify", Path: "b.md", Diff: strings.Repeat("y", 90)},
		},
		Observed: map[string]string{"a.md": "a", "b.md": "b"},
	})
	if !p.Nontrivial() {
		t.Fatal("expected nontrivial")
	}
	rev := &recordingReviewer{fn: func(_ context.Context, model string, _ Packet) (Review, error) {
		return Review{Verdict: VerdictPass, Notes: "", Model: model}, nil
	}}
	out := mustExam(t, rev, Config{Models: []string{"sonnet"}}, p, testIssue())
	if out.Exam.Reasoning != "" {
		t.Fatalf("notes %q", out.Exam.Reasoning)
	}
	if out.Exam.Verdict != VerdictPass {
		t.Fatalf("verdict %s", out.Exam.Verdict)
	}
}

func TestPacketEncodeNeverContainsPlantedTranscript(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	producerTranscript := producerCoT + "\nI decided to rewrite /etc/passwd."
	enc := PacketFrom(p, Issue{Title: "t", Body: "b"}).Encode()
	if strings.Contains(enc, producerTranscript) || strings.Contains(enc, producerCoT) {
		t.Fatal("packet encode contained the planted producer transcript")
	}
}

func TestExamineDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	want := p.Status
	rev := &recordingReviewer{}
	_ = mustExam(t, rev, Config{Models: []string{"sonnet"}}, p, testIssue())
	if p.Status != want || p.CrossExam != nil {
		t.Fatalf("input mutated: %+v", p)
	}
}

func TestApproveRequiresCompletedCrossExam(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{})
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if _, err := p.Approve(now); !errors.Is(err, ErrNotExamined) {
		t.Fatalf("draft approve err=%v want ErrNotExamined", err)
	}
	pending := p
	pending.Status = StatusPendingApproval
	if _, err := pending.Approve(now); !errors.Is(err, ErrNotExamined) {
		t.Fatalf("pending without exam err=%v want ErrNotExamined", err)
	}
	out, err := examined(p).Approve(now)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusApproved {
		t.Fatalf("status %s", out.Status)
	}
	blocked := examined(p)
	blocked.Status = StatusChangesRequested
	if _, err := blocked.Approve(now); err == nil {
		t.Fatal("block-on-fail return must not be approvable")
	}
}
