package server

import (
	"context"
	"fmt"
	"time"

	"github.com/avivl/zeroth/internal/session"
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
	if l, ok := s.lives[id.String()]; ok {
		l.stop()
		delete(s.lives, id.String())
	}
	s.mu.Unlock()
}

func (s *Server) runWorker(ctx context.Context, l *liveRun) {
	defer s.dropWorker(l.id)

	prompt := s.promptOf(ctx, l.id)
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
			if prompt != "" {
				tok = tok + " " + prompt
			}
			if err := s.sup.EmitToken(ctx, l.id, tok); err != nil {
				if ctx.Err() != nil {
					return
				}
				s.log.Debug("worker token", zap.String("run", l.id.String()), zap.Error(err))
				return
			}
		}
	}
	if err := s.sup.Succeed(ctx, l.id); err != nil && ctx.Err() == nil {
		s.log.Debug("worker succeed", zap.String("run", l.id.String()), zap.Error(err))
	}
	if err := s.syncSession(context.Background(), l.id); err != nil {
		s.log.Debug("worker sync", zap.String("run", l.id.String()), zap.Error(err))
	}
}

func (s *Server) promptOf(ctx context.Context, id session.ID) string {
	run, ok, err := s.loadRun(ctx, id.String())
	if err != nil || !ok || run.Prompt == nil {
		return ""
	}
	return *run.Prompt
}
