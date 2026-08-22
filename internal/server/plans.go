package server

import (
	"errors"
	"net/http"

	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func (s *Server) ListPlans(w http.ResponseWriter, r *http.Request, params gen.ListPlansParams) {
	q := store.PlanQuery{}
	if params.Limit != nil {
		q.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		q.Cursor = string(*params.Cursor)
	}
	if params.Status != nil {
		q.Status = string(*params.Status)
	}
	if params.RunId != nil {
		sid, err := store.ParseSessionID(string(*params.RunId))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		q.SessionID = sid
	}
	page, err := s.store.ListPlans(r.Context(), q)
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	items := make([]gen.Plan, 0, len(page.Items))
	for _, rec := range page.Items {
		items = append(items, planFrom(rec))
	}
	out := gen.PlanList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetPlan(w http.ResponseWriter, r *http.Request, id gen.PlanID) {
	pid, err := store.ParsePlanID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	rec, err := s.store.GetPlan(r.Context(), pid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, planFrom(rec))
}
