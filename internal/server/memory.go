package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func (s *Server) notebook() *memory.Notebook {
	nb, err := memory.NewNotebook(s.store, serverAuditor{log: s.audit})
	if err != nil {
		return nil
	}
	return nb
}

type serverAuditor struct {
	log *audit.Log
}

func (a serverAuditor) Append(ctx context.Context, action, target, resourceID, actor string) error {
	if a.log == nil {
		return nil
	}
	agentID, err := store.ParseAgentID(DefaultAgentID)
	if err != nil {
		return err
	}
	_, err = a.log.Append(ctx, audit.Entry{
		Action:       action,
		Target:       target,
		Approver:     actor,
		AgentID:      agentID,
		ResourceType: "memory",
		ResourceID:   resourceID,
	})
	return err
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
		if errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	items := make([]gen.MemoryEntry, 0, len(page.Items))
	for _, m := range page.Items {
		if m.Deleted {
			continue
		}
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
	if !req.Kind.Valid() {
		writeError(w, http.StatusBadRequest, "bad_request", "kind is required")
		return
	}
	kind := string(req.Kind)
	refID := ""
	if req.RefId != nil {
		refID = strings.TrimSpace(*req.RefId)
	}
	if (kind == memory.KindSession || kind == memory.KindAgent) && refID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "ref_id is required")
		return
	}
	key, body := memory.SplitKeyBody(req.Content)
	if body == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "content is required")
		return
	}
	if key == "" {
		id, err := memory.NewFactID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		key = id.String()
	}
	nb := s.notebook()
	if nb == nil {
		writeError(w, http.StatusInternalServerError, "internal", "notebook unavailable")
		return
	}
	fact, err := nb.Write(r.Context(), memory.Human(audit.ApproverOperator), kind, refID, key, body, "operator")
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if errors.Is(err, memory.ErrAgentCannotWrite) {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, memoryFrom(store.MemoryEntry{
		ID:        fact.ID,
		Kind:      fact.Kind,
		RefID:     fact.RefID,
		Content:   fact.Body,
		CreatedAt: fact.CreatedAt,
	}))
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
		if errors.Is(err, store.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	items := make([]gen.MemoryProposal, 0, len(page.Items))
	for _, p := range page.Items {
		items = append(items, storeProposalFrom(p))
	}
	out := gen.MemoryProposalList{Items: items}
	if page.Next != "" {
		c := page.Next
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) AcceptMemoryProposal(w http.ResponseWriter, r *http.Request, id gen.MemoryProposalID) {
	s.decideProposal(w, r, id, true)
}

func (s *Server) RejectMemoryProposal(w http.ResponseWriter, r *http.Request, id gen.MemoryProposalID) {
	s.decideProposal(w, r, id, false)
}

func (s *Server) decideProposal(w http.ResponseWriter, r *http.Request, raw gen.MemoryProposalID, accept bool) {
	pid, err := store.ParseMemoryProposalID(string(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	nb := s.notebook()
	if nb == nil {
		writeError(w, http.StatusInternalServerError, "internal", "notebook unavailable")
		return
	}
	var p memory.Proposal
	if accept {
		p, _, err = nb.Accept(r.Context(), memory.Human(audit.ApproverOperator), pid)
	} else {
		p, err = nb.Reject(r.Context(), memory.Human(audit.ApproverOperator), pid)
	}
	if err != nil {
		switch {
		case errors.Is(err, memory.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "proposal not found")
		case errors.Is(err, memory.ErrNotPending):
			writeError(w, http.StatusConflict, "conflict", err.Error())
		case errors.Is(err, memory.ErrAgentCannotWrite):
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, proposalFromMem(p))
}

func memoryFrom(m store.MemoryEntry) gen.MemoryEntry {
	out := gen.MemoryEntry{
		Id:        gen.MemoryID(m.ID.String()),
		Kind:      gen.MemoryKind(m.Kind),
		Content:   m.Content,
		CreatedAt: m.CreatedAt.UTC(),
	}
	if m.RefID != "" {
		ref := m.RefID
		out.RefId = &ref
	}
	return out
}

func storeProposalFrom(p store.MemoryProposal) gen.MemoryProposal {
	out := gen.MemoryProposal{
		Id:        gen.MemoryProposalID(p.ID.String()),
		Kind:      gen.MemoryKind(p.Kind),
		Content:   p.Content,
		Status:    gen.MemoryProposalStatus(p.Status),
		CreatedAt: p.CreatedAt.UTC(),
	}
	if p.RefID != "" {
		ref := p.RefID
		out.RefId = &ref
	}
	if !p.SessionID.IsZero() {
		rid := gen.RunID(p.SessionID.String())
		out.RunId = &rid
	}
	if !p.MemoryID.IsZero() {
		mid := gen.MemoryID(p.MemoryID.String())
		out.MemoryId = &mid
	}
	if !p.ReviewedAt.IsZero() {
		t := p.ReviewedAt.UTC()
		out.ReviewedAt = &t
	}
	return out
}

func proposalFromMem(p memory.Proposal) gen.MemoryProposal {
	return storeProposalFrom(store.MemoryProposal{
		ID:         p.ID,
		Kind:       p.Kind,
		RefID:      p.RefID,
		SessionID:  p.SessionID,
		Content:    p.Body,
		Status:     p.Status,
		MemoryID:   p.MemoryID,
		CreatedAt:  p.CreatedAt,
		ReviewedAt: p.ReviewedAt,
	})
}
