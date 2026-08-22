package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
)

type crossExamPayload struct {
	PlanID        string `json:"plan_id"`
	Verdict       string `json:"verdict"`
	Notes         string `json:"notes"`
	ReviewerModel string `json:"reviewer_model"`
}

// ExamineDraft runs automatic cross-exam on a stored draft. It is not an
// operator endpoint: the daemon calls this after every new draft.
func (s *Server) ExamineDraft(ctx context.Context, planID store.PlanID) (plan.Outcome, error) {
	if s.reviewer == nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", plan.ErrNoReviewer)
	}
	rec, err := s.store.GetPlan(ctx, planID)
	if err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}
	p, err := plan.FromRecord(rec)
	if err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}
	sess, err := s.store.GetSession(ctx, rec.SessionID)
	if err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}
	agent, err := s.store.GetAgent(ctx, sess.AgentID)
	if err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}
	cfg, err := reviewerConfig(agent)
	if err != nil {
		return plan.Outcome{}, err
	}
	// Independent context: tracker ref plus the operator prompt and
	// the plan rows. The session event log is the producer's
	// transcript and is never an argument here.
	packet := plan.PacketFrom(p, plan.Issue{
		Ref:   sess.TrackerRef,
		Title: sess.TrackerRef,
		Body:  sess.Prompt,
	})
	ex := &plan.Examiner{Reviewer: s.reviewer}
	out, err := ex.Examine(ctx, p, cfg, packet)
	if err != nil {
		return plan.Outcome{}, err
	}
	stored, err := out.Plan.Record()
	if err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}
	if err := s.store.UpdatePlan(ctx, stored); err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}

	sid, err := session.ParseID(sess.ID.String())
	if err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}
	payload, err := json.Marshal(crossExamPayload{
		PlanID:        p.ID.String(),
		Verdict:       out.Exam.Verdict,
		Notes:         out.Exam.Reasoning,
		ReviewerModel: out.Exam.ReviewerModel,
	})
	if err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}
	if err := s.sup.RecordCrossExam(ctx, sid, string(payload)); err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
	}
	if out.Returned {
		if err := s.sup.Steer(ctx, sid, out.SteerMessage()); err != nil {
			return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
		}
	} else {
		if err := s.sup.RequestApproval(ctx, sid, p.ID.String()); err != nil {
			return plan.Outcome{}, fmt.Errorf("server exam: %w", err)
		}
	}

	if _, err := s.audit.Append(ctx, audit.Entry{
		Action:        audit.ActionPlanCrossExam,
		Target:        p.ID.String(),
		PlanHash:      string(p.Hash),
		Postcondition: out.Exam.Verdict,
		Precondition:  out.Exam.ReviewerModel,
		Approver:      out.Exam.ReviewerModel,
		AgentID:       sess.AgentID,
		SessionID:     sess.ID,
		ResourceType:  "plan",
		ResourceID:    p.ID.String(),
	}); err != nil {
		return plan.Outcome{}, fmt.Errorf("server exam: sign: %w", err)
	}
	return out, nil
}

func reviewerConfig(a store.Agent) (plan.Config, error) {
	var models []string
	if m := strings.TrimSpace(a.ReviewerModel); m != "" {
		models = append(models, m)
	}
	if a.ReviewerDual {
		second := strings.TrimSpace(a.ReviewerModel2)
		if second == "" {
			return plan.Config{}, fmt.Errorf("server exam: dual reviewer missing second model: %w", plan.ErrInvalid)
		}
		models = append(models, second)
	}
	return plan.Config{
		ProducerModel: a.Model,
		Models:        models,
		BlockOnFail:   a.BlockOnFail,
	}, nil
}
