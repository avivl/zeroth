package server

import (
	"errors"
	"net/http"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func (s *Server) ListAudit(w http.ResponseWriter, r *http.Request, params gen.ListAuditParams) {
	q := store.AuditQuery{}
	if params.Limit != nil {
		q.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		q.Cursor = string(*params.Cursor)
	}
	if params.ResourceType != nil {
		q.ResourceType = string(*params.ResourceType)
	}
	if params.ResourceId != nil {
		q.ResourceID = *params.ResourceId
	}
	page, err := s.store.ListAudit(r.Context(), q)
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	items := make([]gen.AuditRecord, 0, len(page.Items))
	for _, rec := range page.Items {
		items = append(items, auditFrom(rec))
	}
	out := gen.AuditList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) VerifyAudit(w http.ResponseWriter, r *http.Request, id gen.AuditID) {
	aid, err := store.ParseAuditID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	rec, err := s.store.GetAudit(r.Context(), aid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "audit record not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	keys, err := s.store.ListAgentKeys(r.Context(), rec.AgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := gen.AuditVerification{Id: gen.AuditID(rec.ID.String()), Valid: true}
	if err := audit.VerifyRecord(rec, keys); err != nil {
		out.Valid = false
		msg := err.Error()
		out.Reason = &msg
	}
	writeJSON(w, http.StatusOK, out)
}
