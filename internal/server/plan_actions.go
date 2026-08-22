package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func (s *Server) ApprovePlan(w http.ResponseWriter, r *http.Request, id gen.PlanID) {
	var req gen.ApproveRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json")
			return
		}
	}
	rec, p, err := s.loadPlan(r.Context(), string(id))
	if err != nil {
		writePlanError(w, err)
		return
	}
	approved, err := p.Approve(time.Now().UTC())
	if err != nil {
		writePlanError(w, err)
		return
	}
	if req.Comment != nil {
		approved.ReviewComment = *req.Comment
	}
	stored, err := approved.Record()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.store.UpdatePlan(r.Context(), stored); err != nil {
		writeStoreError(w, err)
		return
	}
	s.markApprovals(r.Context(), rec.ID, rec.SessionID, string(gen.ApprovalStatusApproved))
	sess, err := s.store.GetSession(r.Context(), rec.SessionID)
	if err == nil {
		_, _ = s.audit.Append(r.Context(), audit.Entry{
			Action:       audit.ActionPlanApprove,
			Target:       rec.ID.String(),
			PlanHash:     stored.Hash,
			Approver:     audit.ApproverOperator,
			AgentID:      sess.AgentID,
			SessionID:    rec.SessionID,
			ResourceType: "plan",
			ResourceID:   rec.ID.String(),
		})
	}
	writeJSON(w, http.StatusOK, planFrom(stored))
}

func (s *Server) RequestPlanChanges(w http.ResponseWriter, r *http.Request, id gen.PlanID) {
	var req gen.RequestChangesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	if req.Comment == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "comment is required")
		return
	}
	rec, _, err := s.loadPlan(r.Context(), string(id))
	if err != nil {
		writePlanError(w, err)
		return
	}
	if rec.Status != string(plan.StatusPendingApproval) && rec.Status != string(plan.StatusDraft) {
		writeError(w, http.StatusConflict, "conflict", "plan is not awaiting changes")
		return
	}
	now := time.Now().UTC()
	rec.Status = string(plan.StatusChangesRequested)
	rec.ReviewComment = req.Comment
	rec.UpdatedAt = now
	if err := s.store.UpdatePlan(r.Context(), rec); err != nil {
		writeStoreError(w, err)
		return
	}
	sid, err := session.ParseID(rec.SessionID.String())
	if err == nil {
		if err := s.sup.RequestChanges(r.Context(), sid); err != nil && !errors.Is(err, session.ErrIllegalTransition) {
			status, code, msg := statusForSessionError(err)
			writeError(w, status, code, msg)
			return
		}
		_ = s.syncSession(r.Context(), sid)
	}
	s.markApprovals(r.Context(), rec.ID, rec.SessionID, string(gen.ApprovalStatusChangesRequested))
	writeJSON(w, http.StatusOK, planFrom(rec))
}

func (s *Server) BranchPlan(w http.ResponseWriter, r *http.Request, id gen.PlanID) {
	var req gen.BranchPlanRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json")
			return
		}
	}
	src, _, err := s.loadPlan(r.Context(), string(id))
	if err != nil {
		writePlanError(w, err)
		return
	}
	nid, err := plan.NewID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pid, err := store.ParsePlanID(nid.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	branch := src
	branch.ID = pid
	branch.ParentPlanID = src.ID
	branch.Status = string(plan.StatusDraft)
	branch.CrossExam = nil
	branch.ReviewComment = ""
	if req.Note != nil {
		branch.ReviewComment = *req.Note
	}
	branch.CreatedAt = now
	branch.UpdatedAt = now
	if err := s.store.CreatePlan(r.Context(), branch); err != nil {
		writeStoreError(w, err)
		return
	}
	sess, err := s.store.GetSession(r.Context(), src.SessionID)
	if err == nil {
		_, _ = s.audit.Append(r.Context(), audit.Entry{
			Action:       audit.ActionPlanBranch,
			Target:       pid.String(),
			PlanHash:     branch.Hash,
			Approver:     audit.ApproverOperator,
			AgentID:      sess.AgentID,
			SessionID:    src.SessionID,
			ResourceType: "plan",
			ResourceID:   pid.String(),
		})
	}
	if s.reviewer != nil {
		if _, err := s.ExamineDraft(r.Context(), pid); err == nil {
			got, err := s.store.GetPlan(r.Context(), pid)
			if err == nil {
				branch = got
			}
		}
	}
	writeJSON(w, http.StatusCreated, planFrom(branch))
}

func (s *Server) ApplyPlan(w http.ResponseWriter, r *http.Request, id gen.PlanID) {
	rec, p, err := s.loadPlan(r.Context(), string(id))
	if err != nil {
		writePlanError(w, err)
		return
	}
	if len(rec.SecretScanFindings) > 0 {
		writeError(w, http.StatusConflict, "conflict", "secret-scan findings block apply")
		return
	}
	sess, err := s.store.GetSession(r.Context(), rec.SessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	sid, err := session.ParseID(rec.SessionID.String())
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.sup.BeginApply(r.Context(), sid); err != nil {
		status, code, msg := statusForSessionError(err)
		writeError(w, status, code, msg)
		return
	}
	result, auditID, err := s.applyApproved(r.Context(), sess, p)
	if err != nil {
		reason := err.Error()
		_ = s.sup.Fail(r.Context(), sid, reason)
		s.commentRunFailed(r.Context(), sid, reason)
		_ = s.syncSession(r.Context(), sid)
		writePlanError(w, err)
		return
	}
	stored, err := result.Plan.Record()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.store.UpdatePlan(r.Context(), stored); err != nil {
		writeStoreError(w, err)
		return
	}
	if result.Status == plan.StatusApplied {
		_ = s.sup.Succeed(r.Context(), sid)
		s.completeTracker(r.Context(), sid)
	}
	_ = s.syncSession(r.Context(), sid)
	writeJSON(w, http.StatusOK, gen.ApplyPlanResponse{
		Plan:    planFrom(stored),
		AuditId: gen.AuditID(auditID),
	})
}

func (s *Server) loadPlan(ctx context.Context, raw string) (store.Plan, plan.Plan, error) {
	pid, err := store.ParsePlanID(raw)
	if err != nil {
		return store.Plan{}, plan.Plan{}, err
	}
	rec, err := s.store.GetPlan(ctx, pid)
	if err != nil {
		return store.Plan{}, plan.Plan{}, err
	}
	p, err := plan.FromRecord(rec)
	if err != nil {
		return rec, plan.Plan{}, err
	}
	return rec, p, nil
}

func (s *Server) markApprovals(ctx context.Context, planID store.PlanID, _ store.SessionID, status string) {
	page, err := s.store.ListApprovals(ctx, store.ApprovalQuery{
		Status:    string(gen.ApprovalStatusPending),
		PageQuery: store.PageQuery{Limit: 200},
	})
	if err != nil {
		return
	}
	for _, a := range page.Items {
		if a.PlanID != planID {
			continue
		}
		a.Status = status
		_ = s.store.UpdateApproval(ctx, a)
	}
}

func writePlanError(w http.ResponseWriter, err error) {
	var st *plan.StatusError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, store.ErrInvalid), errors.Is(err, plan.ErrInvalid), errors.Is(err, plan.ErrHashMismatch):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, plan.ErrNotExamined), errors.Is(err, plan.ErrNotApproved),
		errors.Is(err, plan.ErrExpired), errors.Is(err, plan.ErrSecret),
		errors.Is(err, plan.ErrApproval), errors.Is(err, plan.ErrStale),
		errors.Is(err, plan.ErrPostcondition),
		errors.Is(err, plan.ErrDenied), errors.Is(err, plan.ErrPartial),
		errors.As(err, &st):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
