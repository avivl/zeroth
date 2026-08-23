package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/sandbox"
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
		status, code, msg := statusForCheckpointError(err)
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
	sbx, err := s.restoreSandbox(r.Context(), fork, ck.Location)
	if err != nil {
		status, code, msg := statusForCheckpointError(err)
		writeError(w, status, code, msg)
		return
	}
	if err := s.store.CreateSession(r.Context(), fork); err != nil {
		s.stopSandboxID(sbx)
		writeStoreError(w, err)
		return
	}
	if err := s.sup.StartWith(r.Context(), nid); err != nil {
		s.stopSandboxID(sbx)
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.rememberSandbox(nid.String(), sbx)
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
	if err := s.syncSession(r.Context(), nid); err != nil {
		s.log.Debug("restore sync", zap.Error(err))
	}
	s.startWorker(nid)
	run, ok, err := s.loadRun(r.Context(), nid.String())
	if err != nil || !ok {
		writeError(w, http.StatusInternalServerError, "internal", "run forked but not readable")
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func statusForCheckpointError(err error) (int, string, string) {
	switch {
	case errors.Is(err, sandbox.ErrSecret):
		return http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, sandbox.ErrNotFound), errors.Is(err, sandbox.ErrStopped):
		return http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, errCheckpointNoSandbox):
		return http.StatusConflict, "conflict", err.Error()
	default:
		return statusForSessionError(err)
	}
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
