package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"go.uber.org/zap"
)

func (s *Server) ListApprovals(w http.ResponseWriter, r *http.Request, params gen.ListApprovalsParams) {
	q := store.ApprovalQuery{}
	if params.Limit != nil {
		q.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		q.Cursor = string(*params.Cursor)
	}
	if params.Status != nil {
		q.Status = string(*params.Status)
	}
	page, err := s.store.ListApprovals(r.Context(), q)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := make([]gen.Approval, 0, len(page.Items))
	for _, a := range page.Items {
		items = append(items, approvalFrom(a))
	}
	out := gen.ApprovalList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ListMemory(w http.ResponseWriter, r *http.Request, params gen.ListMemoryParams) {
	q := store.MemoryQuery{}
	if params.Limit != nil {
		q.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		q.Cursor = string(*params.Cursor)
	}
	if params.Kind != nil {
		q.Kind = string(*params.Kind)
	}
	if params.RefId != nil {
		q.RefID = *params.RefId
	}
	page, err := s.store.ListMemory(r.Context(), q)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := make([]gen.MemoryEntry, 0, len(page.Items))
	for _, m := range page.Items {
		items = append(items, memoryFrom(m))
	}
	out := gen.MemoryList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateMemory(w http.ResponseWriter, r *http.Request) {
	var req gen.CreateMemoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "content is required")
		return
	}
	id, err := newMemoryID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	entry := store.MemoryEntry{
		ID:        id,
		Kind:      string(req.Kind),
		Content:   req.Content,
		CreatedAt: now,
	}
	if req.RefId != nil {
		entry.RefID = *req.RefId
	}
	if err := s.store.CreateMemory(r.Context(), entry); err != nil {
		writeStoreError(w, err)
		return
	}
	agentID, err := store.ParseAgentID(DefaultAgentID)
	if err == nil {
		_, _ = s.audit.Append(r.Context(), audit.Entry{
			Action:       audit.ActionMemoryCreate,
			Target:       id.String(),
			Approver:     audit.ApproverOperator,
			AgentID:      agentID,
			ResourceType: "memory",
			ResourceID:   id.String(),
		})
	}
	writeJSON(w, http.StatusCreated, memoryFrom(entry))
}

func (s *Server) ListMemoryProposals(w http.ResponseWriter, r *http.Request, params gen.ListMemoryProposalsParams) {
	q := store.MemoryProposalQuery{}
	if params.Limit != nil {
		q.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		q.Cursor = string(*params.Cursor)
	}
	if params.Status != nil {
		q.Status = string(*params.Status)
	}
	page, err := s.store.ListMemoryProposals(r.Context(), q)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := make([]gen.MemoryProposal, 0, len(page.Items))
	for _, p := range page.Items {
		items = append(items, memoryProposalFrom(p))
	}
	out := gen.MemoryProposalList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) AcceptMemoryProposal(w http.ResponseWriter, r *http.Request, id gen.MemoryProposalID) {
	s.reviewMemoryProposal(w, r, id, true)
}

func (s *Server) RejectMemoryProposal(w http.ResponseWriter, r *http.Request, id gen.MemoryProposalID) {
	s.reviewMemoryProposal(w, r, id, false)
}

func (s *Server) reviewMemoryProposal(w http.ResponseWriter, r *http.Request, id gen.MemoryProposalID, accept bool) {
	pid, err := store.ParseMemoryProposalID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p, err := s.store.GetMemoryProposal(r.Context(), pid)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if p.Status != string(gen.MemoryProposalStatusPending) {
		writeError(w, http.StatusConflict, "conflict", "proposal is not pending")
		return
	}
	now := time.Now().UTC()
	p.ReviewedAt = now
	action := audit.ActionMemoryReject
	if accept {
		mid, err := newMemoryID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if err := s.store.CreateMemory(r.Context(), store.MemoryEntry{
			ID:        mid,
			Kind:      p.Kind,
			RefID:     p.RefID,
			Content:   p.Content,
			CreatedAt: now,
		}); err != nil {
			writeStoreError(w, err)
			return
		}
		p.Status = string(gen.MemoryProposalStatusAccepted)
		p.MemoryID = mid
		action = audit.ActionMemoryAccept
	} else {
		p.Status = string(gen.MemoryProposalStatusRejected)
	}
	if err := s.store.UpdateMemoryProposal(r.Context(), p); err != nil {
		writeStoreError(w, err)
		return
	}
	agentID, _ := store.ParseAgentID(DefaultAgentID)
	if !p.SessionID.IsZero() {
		if sess, err := s.store.GetSession(r.Context(), p.SessionID); err == nil {
			agentID = sess.AgentID
		}
	}
	if !agentID.IsZero() {
		_, _ = s.audit.Append(r.Context(), audit.Entry{
			Action:       action,
			Target:       p.ID.String(),
			Approver:     audit.ApproverOperator,
			AgentID:      agentID,
			SessionID:    p.SessionID,
			ResourceType: "memory",
			ResourceID:   p.ID.String(),
		})
	}
	writeJSON(w, http.StatusOK, memoryProposalFrom(p))
}

func (s *Server) ListAgentLeases(w http.ResponseWriter, r *http.Request, id gen.AgentID) {
	aid, err := store.ParseAgentID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if _, err := s.store.GetAgent(r.Context(), aid); err != nil {
		writeStoreError(w, err)
		return
	}
	page, err := s.store.ListLeases(r.Context(), store.LeaseQuery{AgentID: aid})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := make([]gen.Lease, 0, len(page.Items))
	for _, l := range page.Items {
		items = append(items, leaseFrom(l))
	}
	writeJSON(w, http.StatusOK, gen.LeaseList{Items: items})
}

func (s *Server) ListCheckpoints(w http.ResponseWriter, r *http.Request, params gen.ListCheckpointsParams) {
	q := store.CheckpointQuery{}
	if params.Limit != nil {
		q.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		q.Cursor = string(*params.Cursor)
	}
	if params.RunId != nil {
		sid, err := store.ParseSessionID(string(*params.RunId))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		q.SessionID = sid
	}
	page, err := s.store.ListCheckpoints(r.Context(), q)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := make([]gen.Checkpoint, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, checkpointFrom(c))
	}
	out := gen.CheckpointList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateRunCheckpoint(w http.ResponseWriter, r *http.Request, id gen.RunID) {
	var req gen.CreateCheckpointRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid json")
			return
		}
	}
	sid, err := session.ParseID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if _, err := s.sup.State(r.Context(), sid); err != nil {
		status, code, msg := statusForSessionError(err)
		writeError(w, status, code, msg)
		return
	}
	ck, err := s.snapshotRun(r.Context(), sid, strOr(req.Label, "on-demand"))
	if err != nil {
		status, code, msg := statusForSessionError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusCreated, checkpointFrom(ck))
}

func (s *Server) RestoreCheckpoint(w http.ResponseWriter, r *http.Request, id gen.CheckpointID) {
	cid, err := store.ParseCheckpointID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ck, err := s.store.GetCheckpoint(r.Context(), cid)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	src, err := s.store.GetSession(r.Context(), ck.SessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	nid, err := session.NewID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	sid, err := store.ParseSessionID(nid.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	fork := store.Session{
		ID:           sid,
		AgentID:      src.AgentID,
		Status:       string(gen.RunStatusPending),
		Prompt:       src.Prompt,
		TrackerRef:   src.TrackerRef,
		Workspace:    src.Workspace,
		AutonomyTier: src.AutonomyTier,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.CreateSession(r.Context(), fork); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.sup.StartWith(r.Context(), nid); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if _, err := s.audit.Append(r.Context(), audit.Entry{
		Action:       audit.ActionCheckpointRest,
		Target:       sid.String(),
		Precondition: ck.ID.String(),
		Approver:     audit.ApproverOperator,
		AgentID:      src.AgentID,
		SessionID:    sid,
		ResourceType: "checkpoint",
		ResourceID:   ck.ID.String(),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.startWorker(nid)
	if err := s.syncSession(r.Context(), nid); err != nil {
		s.log.Debug("restore sync", zap.Error(err))
	}
	run, ok, err := s.loadRun(r.Context(), nid.String())
	if err != nil || !ok {
		writeError(w, http.StatusInternalServerError, "internal", "run forked but not readable")
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) snapshotRun(ctx context.Context, id session.ID, label string) (store.Checkpoint, error) {
	cid, err := newCheckpointID()
	if err != nil {
		return store.Checkpoint{}, err
	}
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return store.Checkpoint{}, err
	}
	now := time.Now().UTC()
	ck := store.Checkpoint{
		ID:        cid,
		SessionID: sid,
		Label:     label,
		Location:  cid.String(),
		CreatedAt: now,
	}
	if err := s.store.CreateCheckpoint(ctx, ck); err != nil {
		return store.Checkpoint{}, fmt.Errorf("checkpoint: %w", err)
	}
	if err := s.sup.TakeCheckpoint(ctx, id, cid.String()); err != nil {
		return store.Checkpoint{}, fmt.Errorf("checkpoint event: %w", err)
	}
	sess, err := s.store.GetSession(ctx, sid)
	if err == nil {
		_, _ = s.audit.Append(ctx, audit.Entry{
			Action:       audit.ActionCheckpoint,
			Target:       cid.String(),
			Approver:     audit.ApproverOperator,
			AgentID:      sess.AgentID,
			SessionID:    sid,
			ResourceType: "checkpoint",
			ResourceID:   cid.String(),
		})
	}
	return ck, nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func strOr(p *string, fallback string) string {
	if p != nil && *p != "" {
		return *p
	}
	return fallback
}
