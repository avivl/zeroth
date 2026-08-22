package server

import (
	"net/http"

	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func notImplemented(w http.ResponseWriter) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "this path is not implemented in this milestone")
}

func (s *Server) ListAgentLeases(w http.ResponseWriter, _ *http.Request, _ gen.AgentID) {
	notImplemented(w)
}

func (s *Server) ListApprovals(w http.ResponseWriter, _ *http.Request, _ gen.ListApprovalsParams) {
	notImplemented(w)
}

func (s *Server) ListCheckpoints(w http.ResponseWriter, _ *http.Request, _ gen.ListCheckpointsParams) {
	notImplemented(w)
}

func (s *Server) RestoreCheckpoint(w http.ResponseWriter, _ *http.Request, _ gen.CheckpointID) {
	notImplemented(w)
}

func (s *Server) ApplyPlan(w http.ResponseWriter, _ *http.Request, _ gen.PlanID) {
	notImplemented(w)
}

func (s *Server) ApprovePlan(w http.ResponseWriter, _ *http.Request, _ gen.PlanID) {
	notImplemented(w)
}

func (s *Server) BranchPlan(w http.ResponseWriter, _ *http.Request, _ gen.PlanID) {
	notImplemented(w)
}

func (s *Server) RequestPlanChanges(w http.ResponseWriter, _ *http.Request, _ gen.PlanID) {
	notImplemented(w)
}

func (s *Server) CreateRunCheckpoint(w http.ResponseWriter, _ *http.Request, _ gen.RunID) {
	notImplemented(w)
}

func (s *Server) StopRun(w http.ResponseWriter, _ *http.Request, _ gen.RunID) {
	notImplemented(w)
}
