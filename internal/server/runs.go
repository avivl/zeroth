package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"go.uber.org/zap"
)

func (s *Server) ListRuns(w http.ResponseWriter, r *http.Request, params gen.ListRunsParams) {
	q := store.SessionQuery{}
	if params.Limit != nil {
		q.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		q.Cursor = string(*params.Cursor)
	}
	if params.Status != nil {
		q.Status = string(*params.Status)
	}
	if params.AgentId != nil {
		aid, err := store.ParseAgentID(string(*params.AgentId))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		q.AgentID = aid
	}
	page, err := s.store.ListSessions(r.Context(), q)
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	items := make([]gen.Run, 0, len(page.Items))
	for _, sess := range page.Items {
		id, err := session.ParseID(sess.ID.String())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		st, err := s.sup.State(r.Context(), id)
		if err != nil {
			// Store row without a log is a defect; skip rather than fail the list.
			s.log.Warn("list runs: missing session log", zap.String("id", id.String()), zap.Error(err))
			continue
		}
		items = append(items, runFrom(sess, st))
	}
	out := gen.RunList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateRun(w http.ResponseWriter, r *http.Request) {
	var req gen.CreateRunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	if req.AgentId == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "agent_id is required")
		return
	}
	aid, err := store.ParseAgentID(string(req.AgentId))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if _, err := s.store.GetAgent(r.Context(), aid); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "agent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	id, err := session.NewID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	sess := store.Session{
		ID:        sid,
		AgentID:   aid,
		Status:    string(gen.RunStatusPending),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if req.Prompt != nil {
		sess.Prompt = *req.Prompt
	}
	if req.TrackerRef != nil {
		sess.TrackerRef = *req.TrackerRef
	}
	if req.WorkspaceSource != nil {
		sess.Workspace.Repo = req.WorkspaceSource.Repo
		if req.WorkspaceSource.Ref != nil {
			sess.Workspace.Ref = *req.WorkspaceSource.Ref
		}
	}
	if err := s.store.CreateSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	sbx, err := s.spawnHydratedSandbox(r.Context(), sess)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.sup.StartWith(r.Context(), id); err != nil {
		s.stopSandboxID(sbx)
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.rememberSandbox(id.String(), sbx)
	if _, err := s.audit.Append(r.Context(), audit.Entry{
		Action:       audit.ActionRunCreate,
		Target:       sid.String(),
		Approver:     audit.ApproverOperator,
		AgentID:      aid,
		SessionID:    sid,
		ResourceType: "run",
		ResourceID:   sid.String(),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.syncSession(r.Context(), id); err != nil {
		s.log.Warn("create run sync", zap.Error(err))
	}
	s.startWorker(id)
	run, ok, err := s.loadRun(r.Context(), id.String())
	if err != nil || !ok {
		writeError(w, http.StatusInternalServerError, "internal", "run created but not readable")
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) GetRun(w http.ResponseWriter, r *http.Request, id gen.RunID) {
	run, ok, err := s.loadRun(r.Context(), string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) BackgroundRun(w http.ResponseWriter, r *http.Request, id gen.RunID) {
	s.mutateRun(w, r, string(id), func(sid session.ID) error {
		return s.sup.Background(r.Context(), sid, nil)
	})
}

func (s *Server) ForegroundRun(w http.ResponseWriter, r *http.Request, id gen.RunID) {
	s.mutateRun(w, r, string(id), func(sid session.ID) error {
		return s.sup.Foreground(r.Context(), sid)
	})
}

func (s *Server) SteerRun(w http.ResponseWriter, r *http.Request, id gen.RunID) {
	var req gen.SteerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "message is required")
		return
	}
	s.mutateRun(w, r, string(id), func(sid session.ID) error {
		if err := s.sup.Steer(r.Context(), sid, req.Message); err != nil {
			return err
		}
		s.notifySteer(sid, req.Message)
		return nil
	})
}

func (s *Server) mutateRun(w http.ResponseWriter, r *http.Request, raw string, fn func(session.ID) error) {
	id, err := session.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := fn(id); err != nil {
		status, code, msg := statusForSessionError(err)
		writeError(w, status, code, msg)
		return
	}
	if err := s.syncSession(r.Context(), id); err != nil {
		s.log.Warn("mutate run sync", zap.String("id", raw), zap.Error(err))
	}
	run, ok, err := s.loadRun(r.Context(), raw)
	if err != nil || !ok {
		writeError(w, http.StatusInternalServerError, "internal", "run updated but not readable")
		return
	}
	writeJSON(w, http.StatusOK, run)
}
