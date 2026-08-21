package spike

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/eventlog"
	"github.com/avivl/zeroth/zeroth-spike/harness"
	"github.com/avivl/zeroth/zeroth-spike/session"
	"github.com/avivl/zeroth/zeroth-spike/supervisor"
)

// ServerConfig is the spike HTTP surface.
type ServerConfig struct {
	FixturesDir string
	DBPath      string
}

// Server is the spike process: fixtures plus the session event log.
type Server struct {
	cfg ServerConfig
	log *eventlog.Log
	sup *supervisor.Supervisor
	mux *http.ServeMux
}

type healthResponse struct {
	Status string `json:"status"`
}

type authResponse struct {
	APIKeyConfigured bool   `json:"api_key_configured"`
	Method           string `json:"method"`
}

type fixtureRow struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
}

type fixturesResponse struct {
	Dir   string       `json:"dir"`
	Items []fixtureRow `json:"items"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type sessionResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Foreground bool   `json:"foreground"`
	Agent      string `json:"agent"`
	Running    bool   `json:"running"`
}

type eventListResponse struct {
	Items []Frame `json:"items"`
}

type createSessionRequest struct {
	Agent      string   `json:"agent"`
	IntervalMs int      `json:"interval_ms"`
	Cmd        string   `json:"cmd"`
	Args       []string `json:"args"`
}

const defaultReplayLast = 50

// NewServer opens the SQLite log and returns the HTTP handler.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("spike server: empty db path")
	}
	log, err := eventlog.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("spike server: %w", err)
	}
	s := &Server{
		cfg: cfg,
		log: log,
		sup: supervisor.New(log),
		mux: http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

// Close stops agents and the log.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	if s.sup != nil {
		s.sup.Close()
	}
	if s.log != nil {
		return s.log.Close()
	}
	return nil
}

// Log returns the event log. Tests and benches use it as the source of truth.
func (s *Server) Log() *eventlog.Log { return s.log }

// Supervisor returns the session supervisor.
func (s *Server) Supervisor() *supervisor.Supervisor { return s.sup }

// ServeHTTP implements [http.Handler].
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	})
	s.mux.HandleFunc("GET /auth", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, authResponse{
			APIKeyConfigured: harness.APIKeyConfiguredBool(),
			Method:           "api_key",
		})
	})
	s.mux.HandleFunc("GET /fixtures", s.handleFixtures)
	s.mux.HandleFunc("POST /sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("POST /sessions/{id}/bg", s.handleBackground)
	s.mux.HandleFunc("POST /sessions/{id}/stop", s.handleStop)
	s.mux.HandleFunc("GET /sessions/{id}/events", s.handleEvents)
}

func (s *Server) handleFixtures(w http.ResponseWriter, _ *http.Request) {
	dir := s.cfg.FixturesDir
	names := []string{"S.tar", "M.tar", "L.tar"}
	items := make([]fixtureRow, 0, len(names))
	for _, name := range names {
		row := fixtureRow{Name: name, Path: filepath.Join(dir, name)}
		if st, err := os.Stat(row.Path); err == nil && !st.IsDir() {
			row.Size = st.Size()
		}
		items = append(items, row)
	}
	writeJSON(w, http.StatusOK, fixturesResponse{Dir: dir, Items: items})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if r.Body != nil {
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid json"})
			return
		}
	}
	agent, err := agentFromRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	id, err := s.sup.Start(agent)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	s.writeSession(w, r.Context(), id, http.StatusCreated)
}

func agentFromRequest(req createSessionRequest) (supervisor.Agent, error) {
	name := strings.TrimSpace(req.Agent)
	if name == "" {
		name = "fake"
	}
	switch name {
	case "fake":
		interval := time.Duration(req.IntervalMs) * time.Millisecond
		if interval <= 0 {
			interval = 20 * time.Millisecond
		}
		return &supervisor.FakeAgent{Interval: interval}, nil
	case "claude":
		return supervisor.ClaudePromptAgent(), nil
	case "cmd":
		if req.Cmd == "" {
			return nil, fmt.Errorf("cmd agent requires cmd")
		}
		return &supervisor.CmdAgent{Path: req.Cmd, Args: req.Args}, nil
	default:
		return nil, fmt.Errorf("unknown agent %q", name)
	}
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	s.writeSession(w, r.Context(), id, http.StatusOK)
}

func (s *Server) handleBackground(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	if err := s.sup.Background(id); err != nil {
		writeJSON(w, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	s.writeSession(w, r.Context(), id, http.StatusOK)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	if err := s.sup.Stop(id); err != nil {
		writeJSON(w, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	s.writeSession(w, r.Context(), id, http.StatusOK)
}

func (s *Server) writeSession(w http.ResponseWriter, ctx context.Context, id session.ID, status int) {
	info, found, err := s.sup.Lookup(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
		return
	}
	writeJSON(w, status, sessionResponse{
		ID:         info.ID.String(),
		Status:     string(info.Status),
		Foreground: info.Foreground,
		Agent:      info.Agent,
		Running:    info.Running,
	})
}

func parseSessionID(w http.ResponseWriter, r *http.Request) (session.ID, bool) {
	id, err := session.ParseID(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return session.ID{}, false
	}
	return id, true
}

func parseLast(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("last")
	if raw == "" {
		return defaultReplayLast, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 1000 {
		return 0, fmt.Errorf("last must be 1..1000")
	}
	return n, nil
}

func writeJSON[T healthResponse | authResponse | fixturesResponse | errorResponse | sessionResponse | eventListResponse](w http.ResponseWriter, status int, v T) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
