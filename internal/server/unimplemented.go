package server

import (
	"net/http"

	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func (s *Server) StopRun(w http.ResponseWriter, _ *http.Request, _ gen.RunID) {
	notImplemented(w)
}

func notImplemented(w http.ResponseWriter) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "this path is not implemented in this milestone")
}
