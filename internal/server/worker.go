package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/tracker"
	"go.uber.org/zap"
)

func (s *Server) startWorker(id session.ID) {
	s.mu.Lock()
	if _, ok := s.lives[id.String()]; ok {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.root)
	l := &liveRun{
		id:    id,
		steer: make(chan string, 8),
		stop:  cancel,
	}
	s.lives[id.String()] = l
	s.mu.Unlock()
	go s.runWorker(ctx, l)
}

func (s *Server) notifySteer(id session.ID, msg string) {
	s.mu.Lock()
	l := s.lives[id.String()]
	s.mu.Unlock()
	if l == nil {
		return
	}
	select {
	case l.steer <- msg:
	default:
	}
}

func (s *Server) dropWorker(id session.ID) {
	s.mu.Lock()
	l := s.lives[id.String()]
	s.mu.Unlock()
	if l != nil {
		s.dropLive(l)
	}
}

// dropLive removes l only if it is still the current worker for the run.
// A restarted turn must not let the previous goroutine's defer cancel it.
func (s *Server) dropLive(l *liveRun) {
	if l == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lives[l.id.String()] != l {
		return
	}
	l.stop()
	delete(s.lives, l.id.String())
}

func (s *Server) runWorker(ctx context.Context, l *liveRun) {
	defer s.dropLive(l)
	if s.harness != nil {
		s.runHarnessTurn(ctx, l)
		return
	}
	s.runStandinWorker(ctx, l)
}

// runStandinWorker is the test event-log pump. It must not look like a
// successful agent pass: it emits numbered tokens (not the issue prompt)
// and fails once the budget is spent, because it never drafts a plan.
func (s *Server) runStandinWorker(ctx context.Context, l *liveRun) {
	s.log.Warn("run using token stand-in; no harness is configured, so this run cannot produce a plan",
		zap.String("run", l.id.String()))
	tick := time.NewTicker(s.interval)
	defer tick.Stop()
	remaining := s.tokens
	n := 0
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return
		case msg := <-l.steer:
			if err := s.sup.EmitToken(ctx, l.id, "steer-ack: "+msg); err != nil {
				if ctx.Err() != nil {
					return
				}
				s.log.Debug("worker steer-ack", zap.String("run", l.id.String()), zap.Error(err))
			}
		case <-tick.C:
			st, err := s.sup.State(ctx, l.id)
			if err != nil || st.Status.Terminal() {
				return
			}
			if st.Status != session.StatusRunning && st.Status != session.StatusApplying {
				continue
			}
			n++
			remaining--
			tok := fmt.Sprintf("token-%d", n)
			if err := s.sup.EmitToken(ctx, l.id, tok); err != nil {
				if ctx.Err() != nil {
					return
				}
				s.log.Debug("worker token", zap.String("run", l.id.String()), zap.Error(err))
				return
			}
		}
	}
	s.failRun(l.id, "harness is not configured; run produced no plan")
}

func (s *Server) runHarnessTurn(ctx context.Context, l *liveRun) {
	prompt := s.promptOf(ctx, l.id)
	if strings.TrimSpace(prompt) == "" {
		s.failRun(l.id, "empty prompt: refusing to complete without a plan")
		return
	}
	ws, cleanup, err := s.prepareWorkspace(ctx, l.id)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		s.failRun(l.id, fmt.Sprintf("harness workspace: %v", err))
		return
	}

	s.log.Info("harness plan generation starting",
		zap.String("run", l.id.String()),
		zap.String("harness", s.harness.Name()),
		zap.String("tracker", s.trackerKey(l.id)),
	)

	handle, err := s.harness.Start(ctx, harness.Spec{
		Workspace: ws,
		Prompt:    prompt,
		Env:       harnessEnv(),
	})
	if err != nil {
		s.failRun(l.id, fmt.Sprintf("harness start: %v", err))
		return
	}
	l.harnessID = handle.ID
	defer func() {
		if err := s.harness.Stop(context.Background(), handle.ID); err != nil {
			s.log.Debug("harness stop", zap.String("run", l.id.String()), zap.Error(err))
		}
	}()

	ch, err := s.harness.Stream(ctx, handle.ID)
	if err != nil {
		s.failRun(l.id, fmt.Sprintf("harness stream: %v", err))
		return
	}

	timer := time.NewTimer(harnessTurnTimeout)
	defer timer.Stop()

	drafted := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if !drafted {
				s.failRun(l.id, "harness timed out without proposing a plan")
			}
			return
		case msg := <-l.steer:
			if err := s.harness.Steer(ctx, handle.ID, msg); err != nil {
				s.log.Warn("harness steer", zap.String("run", l.id.String()), zap.Error(err))
			}
		case ev, ok := <-ch:
			if !ok {
				if !drafted {
					s.failRun(l.id, "harness exited without proposing a plan")
				}
				return
			}
			if err := s.handleHarnessEvent(ctx, l.id, ws, ev, &drafted); err != nil {
				if ctx.Err() != nil {
					return
				}
				s.failRun(l.id, err.Error())
				return
			}
			if drafted {
				return
			}
		}
	}
}

func (s *Server) handleHarnessEvent(ctx context.Context, id session.ID, workspace string, ev harness.Event, drafted *bool) error {
	switch ev.Kind {
	case harness.EventToken:
		if ev.Payload == "" {
			return nil
		}
		if err := s.sup.EmitToken(ctx, id, ev.Payload); err != nil {
			return fmt.Errorf("session token: %w", err)
		}
	case harness.EventToolCall:
		if ev.Payload == "" {
			return nil
		}
		if err := s.sup.EmitToolCall(ctx, id, ev.Payload); err != nil {
			return fmt.Errorf("session tool call: %w", err)
		}
	case harness.EventError:
		if ev.Payload == "" {
			return nil
		}
		if err := s.sup.ReportError(ctx, id, ev.Payload); err != nil {
			return fmt.Errorf("session error: %w", err)
		}
	case harness.EventEffects:
		if *drafted {
			s.log.Warn("ignoring extra harness effects; one plan-generation attempt per run",
				zap.String("run", id.String()))
			return nil
		}
		if err := s.draftFromEffects(ctx, id, workspace, ev.Effects); err != nil {
			return err
		}
		*drafted = true
	case harness.EventExited:
		if !*drafted {
			msg := "harness exited without proposing a plan"
			if p := strings.TrimSpace(ev.Payload); p != "" {
				msg = msg + ": " + p
			}
			return fmt.Errorf("%s", msg)
		}
	}
	return nil
}

func (s *Server) failRun(id session.ID, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "run produced no plan"
	}
	s.log.Warn("run failed without a change plan",
		zap.String("run", id.String()),
		zap.String("reason", reason),
		zap.String("tracker", s.trackerKey(id)),
	)
	bg := context.Background()
	st, err := s.sup.State(bg, id)
	if err == nil && st.Status.Terminal() {
		_ = s.syncSession(bg, id)
		return
	}
	if err := s.sup.Fail(bg, id, reason); err != nil {
		s.log.Warn("session fail", zap.String("run", id.String()), zap.Error(err))
	}
	s.commentRunFailed(bg, id, reason)
	s.stopSandbox(id)
	if err := s.syncSession(bg, id); err != nil {
		s.log.Warn("fail sync", zap.String("run", id.String()), zap.Error(err))
	}
}

func (s *Server) commentRunFailed(ctx context.Context, id session.ID, reason string) {
	key := s.trackerKey(id)
	if s.tracker == nil || key == "" {
		return
	}
	if _, err := s.tracker.Comment(ctx, key, tracker.FormatFailedComment(id.String(), reason)); err != nil {
		s.log.Warn("tracker fail comment", zap.String("key", key), zap.Error(err))
	}
}

func (s *Server) promptOf(ctx context.Context, id session.ID) string {
	run, ok, err := s.loadRun(ctx, id.String())
	if err != nil || !ok || run.Prompt == nil {
		return ""
	}
	return *run.Prompt
}

func harnessEnv() []string {
	if k := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); k != "" {
		return []string{"ANTHROPIC_API_KEY=" + k}
	}
	return nil
}
