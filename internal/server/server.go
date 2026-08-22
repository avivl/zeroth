package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/signer"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/tracker"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"go.uber.org/zap"
)

const (
	// DefaultAgentID is the local agent seeded when the daemon starts with
	// an empty agent table. Create-run requires an agent_id and the contract
	// has no POST /agents.
	DefaultAgentID = "a_default"

	defaultReplayLast = 50
	maxReplayLast     = 1000
	defaultTokens     = 120
	defaultInterval   = 100 * time.Millisecond

	// TrackerWebhookPath is mounted only when a webhook secret is configured.
	TrackerWebhookPath = "/webhooks/tracker"
)

// Config is the HTTP surface. Store is required. Signer defaults to an
// in-memory backend when nil (tests). TokenInterval and TokenCount bound
// the in-process stand-in worker that emits log events until the harness
// loop is wired into the daemon. Tracker and Sandbox are ports; the
// daemon injects Linear and Docker by name, this package does not.
type Config struct {
	Store          store.Store
	Signer         signer.Service
	Log            *zap.Logger
	Reviewer       plan.Reviewer
	TokenInterval  time.Duration
	TokenCount     int
	Tracker        tracker.Provider
	Sandbox        sandbox.Driver
	TrackerWebhook bool
}

// Server serves the OpenAPI contract against a session supervisor.
type Server struct {
	store    store.Store
	audit    *audit.Log
	log      *zap.Logger
	reviewer plan.Reviewer
	elog     *storeLog
	sup      *session.Supervisor
	interval time.Duration
	tokens   int
	tracker  tracker.Provider
	sandbox  sandbox.Driver
	webhook  http.Handler

	root   context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	lives     map[string]*liveRun
	byTracker map[string]session.ID
	sandboxes map[string]sandbox.ID
	keys      map[string]string
}

type liveRun struct {
	id    session.ID
	steer chan string
	stop  context.CancelFunc
}

// New opens a supervisor on the store-backed log, seeds the default
// agent, and resumes workers for non-terminal sessions.
func New(cfg Config) (*Server, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("server: nil store")
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	interval := cfg.TokenInterval
	if interval <= 0 {
		interval = defaultInterval
	}
	tokens := cfg.TokenCount
	if tokens <= 0 {
		tokens = defaultTokens
	}
	sg := cfg.Signer
	if sg == nil {
		sg = signer.NewMemory()
	}
	trail, err := audit.NewLog(cfg.Store, sg)
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	elog := newStoreLog(cfg.Store)
	sup, err := session.Restore(context.Background(), elog)
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		store:     cfg.Store,
		audit:     trail,
		log:       log,
		reviewer:  cfg.Reviewer,
		elog:      elog,
		sup:       sup,
		interval:  interval,
		tokens:    tokens,
		tracker:   cfg.Tracker,
		sandbox:   cfg.Sandbox,
		root:      ctx,
		cancel:    cancel,
		lives:     make(map[string]*liveRun),
		byTracker: make(map[string]session.ID),
		sandboxes: make(map[string]sandbox.ID),
		keys:      make(map[string]string),
	}
	if cfg.TrackerWebhook {
		if h, ok := cfg.Tracker.(http.Handler); ok {
			s.webhook = h
		}
	}
	if err := s.ensureDefaultAgent(ctx); err != nil {
		s.Close()
		return nil, err
	}
	for _, id := range sup.LiveIDs() {
		s.rememberTracker(ctx, id)
		s.startWorker(id)
	}
	if s.tracker != nil {
		go s.watchTracker()
	}
	return s, nil
}

// Close stops workers and session goroutines. It does not close the store.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.cancel()
	s.mu.Lock()
	for _, l := range s.lives {
		l.stop()
	}
	s.mu.Unlock()
	s.stopAllSandboxes()
	if s.sup != nil {
		s.sup.Close()
	}
}

// Handler returns the OpenAPI mux. The tracker webhook is opt-in.
func (s *Server) Handler() http.Handler {
	api := gen.Handler(s)
	if s.webhook == nil {
		return api
	}
	mux := http.NewServeMux()
	mux.Handle(TrackerWebhookPath, s.webhook)
	mux.Handle("/", api)
	return mux
}

func (s *Server) ensureDefaultAgent(ctx context.Context) error {
	id, err := store.ParseAgentID(DefaultAgentID)
	if err != nil {
		return fmt.Errorf("server default agent: %w", err)
	}
	created := false
	_, err = s.store.GetAgent(ctx, id)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("server default agent: %w", err)
		}
		now := time.Now().UTC()
		if err := s.store.CreateAgent(ctx, store.Agent{
			ID:        id,
			Name:      "default",
			Harness:   "claudecode",
			Status:    string(gen.Ready),
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("server default agent: %w", err)
		}
		created = true
	}
	if err := s.audit.EnsureAgentKey(ctx, id, created); err != nil {
		return fmt.Errorf("server default agent key: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, gen.Error{Code: code, Message: message})
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("empty body")
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func (s *Server) loadRun(ctx context.Context, raw string) (gen.Run, bool, error) {
	sid, err := store.ParseSessionID(raw)
	if err != nil {
		return gen.Run{}, false, err
	}
	sess, err := s.store.GetSession(ctx, sid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return gen.Run{}, false, nil
		}
		return gen.Run{}, false, err
	}
	id, err := session.ParseID(raw)
	if err != nil {
		return gen.Run{}, false, err
	}
	st, err := s.sup.State(ctx, id)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			return gen.Run{}, false, nil
		}
		return gen.Run{}, false, err
	}
	return runFrom(sess, st), true, nil
}

func (s *Server) syncSession(ctx context.Context, id session.ID) error {
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return fmt.Errorf("server sync session: %w", err)
	}
	sess, err := s.store.GetSession(ctx, sid)
	if err != nil {
		return fmt.Errorf("server sync session: %w", err)
	}
	st, err := s.sup.State(ctx, id)
	if err != nil {
		return fmt.Errorf("server sync session: %w", err)
	}
	sess.Status = string(apiStatus(st))
	sess.UpdatedAt = time.Now().UTC()
	if st.Status.Terminal() && sess.FinishedAt.IsZero() {
		sess.FinishedAt = sess.UpdatedAt
	}
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("server sync session: %w", err)
	}
	return nil
}

func statusForSessionError(err error) (int, string, string) {
	switch {
	case errors.Is(err, session.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, session.ErrIllegalTransition):
		return http.StatusConflict, "conflict", err.Error()
	default:
		return http.StatusInternalServerError, "internal", err.Error()
	}
}

func parseReplayLast(params gen.GetRunEventsParams) (int, error) {
	if params.Last == nil {
		return defaultReplayLast, nil
	}
	n := *params.Last
	if n < 1 || n > maxReplayLast {
		return 0, fmt.Errorf("last must be 1..%d", maxReplayLast)
	}
	return n, nil
}

var _ gen.ServerInterface = (*Server)(nil)
