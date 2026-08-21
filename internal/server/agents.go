package server

import (
	"errors"
	"net/http"

	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func (s *Server) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, gen.Health{Status: gen.Ok})
}

func (s *Server) ListAgents(w http.ResponseWriter, r *http.Request, params gen.ListAgentsParams) {
	q := store.PageQuery{}
	if params.Limit != nil {
		q.Limit = int(*params.Limit)
	}
	if params.Cursor != nil {
		q.Cursor = string(*params.Cursor)
	}
	page, err := s.store.ListAgents(r.Context(), q)
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	items := make([]gen.Agent, 0, len(page.Items))
	for _, a := range page.Items {
		items = append(items, agentFrom(a))
	}
	out := gen.AgentList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetAgent(w http.ResponseWriter, r *http.Request, id gen.AgentID) {
	aid, err := store.ParseAgentID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	a, err := s.store.GetAgent(r.Context(), aid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "agent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, agentFrom(a))
}
