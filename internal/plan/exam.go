package plan

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Config is per-agent reviewer setup. Models is one reviewer, or two
// for dual (both must pass). Every model must differ from the producer
// and from the other reviewer: same-model second pass does not count.
type Config struct {
	ProducerModel string
	Models        []string
	BlockOnFail   bool
}

// Review is one model's verdict on a packet.
type Review struct {
	Verdict string
	Notes   string
	Model   string
}

// Reviewer reads a packet in a fresh context. Implementations must not
// be given the producer's transcript: the packet is the whole input.
type Reviewer interface {
	Review(ctx context.Context, model string, packet Packet) (Review, error)
}

// VerdictAuditor signs a completed cross-exam. The real trail is
// internal/audit; this port keeps plan I/O-free the same way Apply does.
type VerdictAuditor interface {
	SignVerdict(ctx context.Context, p Plan, exam CrossExam) error
}

// Examiner is the automatic challenge that runs after every draft.
type Examiner struct {
	Reviewer Reviewer
	Audit    VerdictAuditor
	Clock    Clock
}

// Outcome is one completed exam. Returned is true when block-on-fail
// sent the plan back to the agent instead of escalating to a human.
type Outcome struct {
	Plan     Plan
	Exam     CrossExam
	Returned bool
}

// SteerMessage is the note the agent sees on a returned plan.
func (o Outcome) SteerMessage() string {
	notes := strings.TrimSpace(o.Exam.Reasoning)
	if notes == "" {
		return "cross-exam failed. Revise the plan."
	}
	return "cross-exam failed:\n" + notes
}

// Examine runs the configured reviewer(s) against packet. p must be a
// draft (or a previous changes-requested revision). It never mutates p.
func (e *Examiner) Examine(ctx context.Context, p Plan, cfg Config, packet Packet) (Outcome, error) {
	if e == nil || e.Reviewer == nil {
		return Outcome{}, fmt.Errorf("plan exam: %w", ErrNoReviewer)
	}
	if err := ctx.Err(); err != nil {
		return Outcome{}, fmt.Errorf("plan exam: %w", err)
	}
	models, err := normalizeModels(cfg)
	if err != nil {
		return Outcome{}, err
	}
	switch p.Status {
	case StatusDraft, StatusChangesRequested, StatusCrossExam:
	default:
		return Outcome{}, fmtStatus("plan exam", p.Status)
	}

	clock := e.Clock
	if clock == nil {
		clock = systemClock{}
	}
	now := clock.Now().UTC()

	out := clonePlan(p)
	out.Status = StatusCrossExam
	out.UpdatedAt = now

	reviews := make([]Review, 0, len(models))
	for _, model := range models {
		rev, err := e.Reviewer.Review(ctx, model, packet)
		if err != nil {
			return Outcome{}, fmt.Errorf("plan exam: reviewer %s: %w", model, err)
		}
		rev.Model = model
		rev.Verdict = normalizeVerdict(rev.Verdict)
		reviews = append(reviews, rev)
	}

	exam := combineReviews(reviews, now)
	out.CrossExam = &exam
	if exam.Verdict == VerdictFail && cfg.BlockOnFail {
		out.Status = StatusChangesRequested
		out.ReviewComment = exam.Reasoning
	} else {
		out.Status = StatusPendingApproval
	}
	out.UpdatedAt = now

	if e.Audit != nil {
		if err := e.Audit.SignVerdict(ctx, out, exam); err != nil {
			return Outcome{}, fmt.Errorf("plan exam: sign: %w", err)
		}
	}

	return Outcome{
		Plan:     out,
		Exam:     exam,
		Returned: exam.Verdict == VerdictFail && cfg.BlockOnFail,
	}, nil
}

func normalizeModels(cfg Config) ([]string, error) {
	producer := strings.TrimSpace(cfg.ProducerModel)
	seen := make(map[string]struct{}, 2)
	var models []string
	for i, raw := range cfg.Models {
		m := strings.TrimSpace(raw)
		if m == "" {
			return nil, fmt.Errorf("plan exam: model %d: %w", i, ErrInvalid)
		}
		if producer != "" && m == producer {
			return nil, fmt.Errorf("plan exam: %w", ErrSameModel)
		}
		if _, ok := seen[m]; ok {
			return nil, fmt.Errorf("plan exam: %w", ErrSameModel)
		}
		seen[m] = struct{}{}
		models = append(models, m)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("plan exam: %w", ErrNoReviewer)
	}
	if len(models) > 2 {
		return nil, fmt.Errorf("plan exam: more than two reviewers: %w", ErrInvalid)
	}
	return models, nil
}

func normalizeVerdict(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case VerdictPass, "ok", "allow":
		return VerdictPass
	case VerdictFail, "deny", "block":
		return VerdictFail
	case VerdictPassWithNotes, "pass-with-notes":
		return VerdictPassWithNotes
	default:
		return VerdictFail
	}
}

func combineReviews(reviews []Review, now time.Time) CrossExam {
	models := make([]string, 0, len(reviews))
	var notes []string
	verdict := VerdictPass
	for _, r := range reviews {
		models = append(models, r.Model)
		if n := strings.TrimSpace(r.Notes); n != "" {
			if len(reviews) > 1 {
				notes = append(notes, r.Model+": "+n)
			} else {
				notes = append(notes, n)
			}
		}
		switch r.Verdict {
		case VerdictFail:
			verdict = VerdictFail
		case VerdictPassWithNotes:
			if verdict != VerdictFail {
				verdict = VerdictPassWithNotes
			}
		}
	}
	return CrossExam{
		Verdict:       verdict,
		ReviewerModel: strings.Join(models, ","),
		Reasoning:     strings.Join(notes, "\n"),
		At:            now,
	}
}
