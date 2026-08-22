package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/tracker"
	"go.uber.org/zap"
)

func (s *Server) watchTracker() {
	err := tracker.Watch(s.root, s.tracker, s)
	if err != nil && s.root.Err() == nil {
		s.log.Warn("tracker watch ended", zap.Error(err))
	}
}

// OnAssigned starts a headless run for an issue assigned to the agent
// (Z1-038). Errors are logged and commented; they do not stop the watch loop.
func (s *Server) OnAssigned(ctx context.Context, ev tracker.AssignmentEvent) error {
	if err := s.handleAssigned(ctx, ev); err != nil {
		s.log.Warn("tracker assigned", zap.String("key", ev.Key), zap.Error(err))
		if s.tracker != nil && ev.Key != "" {
			_, _ = s.tracker.Comment(context.Background(), ev.Key, "Zeroth failed to start this run. See local daemon logs.")
		}
	}
	return nil
}

// OnUnassigned cancels the run and stops the sandbox. Cancellation is not
// a row update alone: Kill then Stop must run (Z1-038).
func (s *Server) OnUnassigned(ctx context.Context, ev tracker.AssignmentEvent) error {
	if err := s.handleUnassigned(ctx, ev); err != nil {
		s.log.Warn("tracker unassigned", zap.String("key", ev.Key), zap.Error(err))
	}
	return nil
}

func (s *Server) handleAssigned(ctx context.Context, ev tracker.AssignmentEvent) error {
	key := strings.TrimSpace(ev.Key)
	if key == "" {
		return fmt.Errorf("server assign: empty key")
	}
	s.mu.Lock()
	if id, ok := s.byTracker[key]; ok && s.sup.Live(id) {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	iss := ev.Issue
	if iss.Key == "" && s.tracker != nil {
		got, err := s.tracker.GetIssue(ctx, key)
		if err != nil {
			s.log.Debug("tracker get issue on assign", zap.String("key", key), zap.Error(err))
		} else {
			iss = got
		}
	}
	prompt := issuePrompt(key, iss)

	id, err := session.NewID()
	if err != nil {
		return fmt.Errorf("server assign: %w", err)
	}
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return fmt.Errorf("server assign: %w", err)
	}
	aid, err := store.ParseAgentID(DefaultAgentID)
	if err != nil {
		return fmt.Errorf("server assign: %w", err)
	}
	now := time.Now().UTC()
	sess := store.Session{
		ID:         sid,
		AgentID:    aid,
		Status:     "pending",
		Prompt:     prompt,
		TrackerRef: key,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	sbx, err := s.spawnHydratedSandbox(ctx, sess)
	if err != nil {
		return err
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		s.stopSandboxID(sbx)
		return fmt.Errorf("server assign: %w", err)
	}
	if err := s.sup.StartWith(ctx, id); err != nil {
		s.stopSandboxID(sbx)
		return fmt.Errorf("server assign: %w", err)
	}
	if _, err := s.audit.Append(ctx, audit.Entry{
		Action:       audit.ActionRunCreate,
		Target:       sid.String(),
		Approver:     audit.ApproverOperator,
		AgentID:      aid,
		SessionID:    sid,
		ResourceType: "run",
		ResourceID:   sid.String(),
	}); err != nil {
		s.stopSandboxID(sbx)
		return fmt.Errorf("server assign: %w", err)
	}
	if err := s.sup.Background(ctx, id, nil); err != nil {
		s.log.Debug("tracker background", zap.String("run", id.String()), zap.Error(err))
	}

	s.mu.Lock()
	s.byTracker[key] = id
	s.keys[id.String()] = key
	s.mu.Unlock()
	s.rememberSandbox(id.String(), sbx)

	s.startWorker(id)
	if err := s.syncSession(ctx, id); err != nil {
		s.log.Debug("tracker assign sync", zap.Error(err))
	}
	if s.tracker != nil {
		_, err := s.tracker.Comment(ctx, key, tracker.FormatStartedComment(id.String(), key))
		if err != nil {
			s.log.Debug("tracker started comment", zap.Error(err))
		}
		if err := s.tracker.SetState(ctx, key, tracker.State{Kind: tracker.StateStarted}); err != nil {
			s.log.Debug("tracker set started", zap.Error(err))
		}
	}
	return nil
}

func (s *Server) handleUnassigned(ctx context.Context, ev tracker.AssignmentEvent) error {
	key := strings.TrimSpace(ev.Key)
	if key == "" {
		return nil
	}
	s.mu.Lock()
	id, ok := s.byTracker[key]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	s.dropWorker(id)
	s.killSandbox(id)
	st, err := s.sup.State(ctx, id)
	if err == nil && !st.Status.Terminal() {
		if err := s.sup.Fail(ctx, id, "unassigned"); err != nil {
			s.log.Debug("tracker unassign fail", zap.Error(err))
		}
	}
	if s.tracker != nil {
		_, _ = s.tracker.Comment(context.Background(), key, tracker.FormatCancelComment(id.String()))
	}
	_ = s.syncSession(context.Background(), id)
	s.forgetTracker(id, key)
	return nil
}

func (s *Server) completeTracker(ctx context.Context, id session.ID) {
	key := s.trackerKey(id)
	if s.tracker == nil || key == "" {
		s.stopSandbox(id)
		return
	}
	line := s.auditLine(ctx, id)
	body := tracker.FormatCompletion(tracker.Completion{
		RunID:      id.String(),
		Transcript: "zeroth attach " + id.String(),
		Audit:      line,
	})
	if _, err := s.tracker.Comment(ctx, key, body); err != nil {
		s.log.Debug("tracker completion comment", zap.Error(err))
	}
	if err := s.tracker.SetState(ctx, key, tracker.State{Kind: tracker.StateCompleted}); err != nil {
		s.log.Debug("tracker set completed", zap.Error(err))
	}
	s.stopSandbox(id)
}

func (s *Server) auditLine(ctx context.Context, id session.ID) string {
	evs, err := s.sup.Events(ctx, id)
	if err != nil {
		return "event log unavailable"
	}
	st, err := s.sup.State(ctx, id)
	status := "unknown"
	if err == nil {
		status = string(st.Status)
	}
	return fmt.Sprintf("%d events, terminal=%s", len(evs), status)
}

func (s *Server) rememberTracker(ctx context.Context, id session.ID) {
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return
	}
	sess, err := s.store.GetSession(ctx, sid)
	if err != nil || sess.TrackerRef == "" {
		return
	}
	s.mu.Lock()
	s.byTracker[sess.TrackerRef] = id
	s.keys[id.String()] = sess.TrackerRef
	s.mu.Unlock()
}

func (s *Server) trackerKey(id session.ID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys[id.String()]
}

func (s *Server) forgetTracker(id session.ID, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, id.String())
	if key != "" {
		delete(s.byTracker, key)
	}
}

func (s *Server) killSandbox(id session.ID) {
	if s.sandbox == nil {
		return
	}
	sbx := s.takeSandbox(id)
	if sbx.IsZero() {
		return
	}
	ctx := context.Background()
	if err := s.sandbox.Kill(ctx, sbx); err != nil {
		s.log.Debug("sandbox kill", zap.String("run", id.String()), zap.Error(err))
	}
	if err := s.sandbox.Stop(ctx, sbx); err != nil {
		s.log.Debug("sandbox stop", zap.String("run", id.String()), zap.Error(err))
	}
}

func (s *Server) stopSandbox(id session.ID) {
	s.stopSandboxID(s.takeSandbox(id))
}

func (s *Server) stopSandboxID(sbx sandbox.ID) {
	if s.sandbox == nil || sbx.IsZero() {
		return
	}
	if err := s.sandbox.Stop(context.Background(), sbx); err != nil {
		s.log.Debug("sandbox stop", zap.Error(err))
	}
}

func (s *Server) takeSandbox(id session.ID) sandbox.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	sbx := s.sandboxes[id.String()]
	delete(s.sandboxes, id.String())
	return sbx
}

func (s *Server) stopAllSandboxes() {
	if s.sandbox == nil {
		return
	}
	s.mu.Lock()
	ids := make([]sandbox.ID, 0, len(s.sandboxes))
	for _, id := range s.sandboxes {
		ids = append(ids, id)
	}
	s.sandboxes = make(map[string]sandbox.ID)
	s.mu.Unlock()
	ctx := context.Background()
	for _, id := range ids {
		_ = s.sandbox.Kill(ctx, id)
		_ = s.sandbox.Stop(ctx, id)
	}
}

func issuePrompt(key string, iss tracker.Issue) string {
	title := strings.TrimSpace(iss.Title)
	if title == "" {
		title = key
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Linear %s: %s", key, title)
	if d := strings.TrimSpace(iss.Description); d != "" {
		b.WriteString("\n\n")
		b.WriteString(d)
	}
	return b.String()
}
