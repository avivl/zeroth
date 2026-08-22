package session

import (
	"context"
	"fmt"
	"sync"
)

// Supervisor owns live sessions. One goroutine per non-terminal session
// serializes mutations. The goroutine is not a second log: after a
// daemon restart, [Restore] rebuilds supervisors from the same events.
type Supervisor struct {
	log Log

	root   context.Context
	cancel context.CancelFunc

	mu   sync.Mutex
	runs map[ID]*run
}

type run struct {
	machine *Machine
	cmds    chan command
	done    chan struct{}
}

type command struct {
	fn   func() error
	errc chan error
}

// NewSupervisor returns an empty supervisor writing to log.
func NewSupervisor(log Log) (*Supervisor, error) {
	if log == nil {
		return nil, fmt.Errorf("session supervisor: nil log")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{
		log:    log,
		root:   ctx,
		cancel: cancel,
		runs:   make(map[ID]*run),
	}, nil
}

// Restore rebuilds a supervisor from log. Non-terminal sessions get a
// goroutine again. Terminal sessions remain readable from the log.
func Restore(ctx context.Context, log Log) (*Supervisor, error) {
	s, err := NewSupervisor(log)
	if err != nil {
		return nil, err
	}
	ids, err := log.SessionIDs(ctx)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("session restore: %w", err)
	}
	for _, id := range ids {
		m, err := Open(id, log)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("session restore: %w", err)
		}
		st, err := m.State(ctx)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("session restore: %w", err)
		}
		if st.Status.Terminal() {
			continue
		}
		s.spawn(m)
	}
	return s, nil
}

// Close stops every session goroutine. It does not append terminal events:
// a later Restore resumes non-terminal sessions from the log.
func (s *Supervisor) Close() {
	s.cancel()
	s.mu.Lock()
	dones := make([]chan struct{}, 0, len(s.runs))
	for _, r := range s.runs {
		dones = append(dones, r.done)
	}
	s.mu.Unlock()
	for _, done := range dones {
		<-done
	}
}

// Start creates a pending session, starts it, and returns the id.
func (s *Supervisor) Start(ctx context.Context) (ID, error) {
	id, err := NewID()
	if err != nil {
		return ID{}, fmt.Errorf("session supervisor start: %w", err)
	}
	if err := s.StartWith(ctx, id); err != nil {
		return ID{}, err
	}
	return id, nil
}

// StartWith starts a session that already has a durable row (the store
// session id). Used by the HTTP create-run path, which must insert the
// session before the first event because events reference sessions.
func (s *Supervisor) StartWith(ctx context.Context, id ID) error {
	if id.IsZero() {
		return fmt.Errorf("session supervisor start: empty id")
	}
	s.mu.Lock()
	_, exists := s.runs[id]
	s.mu.Unlock()
	if exists {
		return fmt.Errorf("session %s: already live", id.String())
	}
	m, err := New(ctx, id, s.log)
	if err != nil {
		return fmt.Errorf("session supervisor start: %w", err)
	}
	if err := m.Start(ctx); err != nil {
		return fmt.Errorf("session supervisor start: %w", err)
	}
	s.spawn(m)
	return nil
}

// LiveIDs returns the ids with a supervisor goroutine still running.
func (s *Supervisor) LiveIDs() []ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ID, 0, len(s.runs))
	for id := range s.runs {
		out = append(out, id)
	}
	return out
}

func (s *Supervisor) spawn(m *Machine) {
	r := &run{
		machine: m,
		cmds:    make(chan command),
		done:    make(chan struct{}),
	}
	s.mu.Lock()
	s.runs[m.ID()] = r
	s.mu.Unlock()
	go s.loop(m.ID(), r)
}

func (s *Supervisor) loop(id ID, r *run) {
	defer close(r.done)
	for {
		select {
		case <-s.root.Done():
			return
		case cmd := <-r.cmds:
			err := cmd.fn()
			cmd.errc <- err
			st, stErr := r.machine.State(context.Background())
			if stErr == nil && st.Status.Terminal() {
				s.mu.Lock()
				if s.runs[id] == r {
					delete(s.runs, id)
				}
				s.mu.Unlock()
				return
			}
		}
	}
}

func (s *Supervisor) do(ctx context.Context, id ID, fn func() error) error {
	s.mu.Lock()
	r, ok := s.runs[id]
	s.mu.Unlock()
	if !ok {
		// Goroutine is gone (terminal, or Close). Run fn on the log-backed
		// machine so callers see illegal-transition or not-found, not a
		// missing supervisor.
		return fn()
	}
	errc := make(chan error, 1)
	cmd := command{fn: fn, errc: errc}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		return fn()
	case r.cmds <- cmd:
	}
	select {
	case err := <-errc:
		return err
	case <-r.done:
		select {
		case err := <-errc:
			return err
		default:
			return fn()
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Supervisor) machine(id ID) (*Machine, error) {
	s.mu.Lock()
	r, ok := s.runs[id]
	s.mu.Unlock()
	if ok {
		return r.machine, nil
	}
	return Open(id, s.log)
}

// Live reports whether a supervisor goroutine is still running for id.
func (s *Supervisor) Live(id ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.runs[id]
	return ok
}

// State replays the log for id.
func (s *Supervisor) State(ctx context.Context, id ID) (State, error) {
	m, err := s.machine(id)
	if err != nil {
		return State{}, err
	}
	return m.State(ctx)
}

// Events returns the chronological log for id.
func (s *Supervisor) Events(ctx context.Context, id ID) ([]Event, error) {
	m, err := s.machine(id)
	if err != nil {
		return nil, err
	}
	return m.Events(ctx)
}

// Tail is [Tail] against this supervisor's log.
func (s *Supervisor) Tail(ctx context.Context, id ID, last int) (replay []Event, live <-chan Event, stop func(), err error) {
	return Tail(ctx, s.log, id, last)
}

// Steer injects operator guidance.
func (s *Supervisor) Steer(ctx context.Context, id ID, msg string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.Steer(ctx, msg)
	})
}

// Background demotes the session. Nil contract uses the default.
func (s *Supervisor) Background(ctx context.Context, id ID, contract *CompletionContract) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.Background(ctx, contract)
	})
}

// Foreground promotes the session.
func (s *Supervisor) Foreground(ctx context.Context, id ID) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.Foreground(ctx)
	})
}

// EmitToken records a token.
func (s *Supervisor) EmitToken(ctx context.Context, id ID, tok string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.EmitToken(ctx, tok)
	})
}

// EmitToolCall records a tool call.
func (s *Supervisor) EmitToolCall(ctx context.Context, id ID, call string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.EmitToolCall(ctx, call)
	})
}

// ProposePlan records a plan draft.
func (s *Supervisor) ProposePlan(ctx context.Context, id ID, ref string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.ProposePlan(ctx, ref)
	})
}

// RecordCrossExam records a reviewer verdict.
func (s *Supervisor) RecordCrossExam(ctx context.Context, id ID, payload string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.RecordCrossExam(ctx, payload)
	})
}

// RequestApproval moves running -> awaiting-approval.
func (s *Supervisor) RequestApproval(ctx context.Context, id ID, ref string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.RequestApproval(ctx, ref)
	})
}

// RequestChanges moves awaiting-approval -> running.
func (s *Supervisor) RequestChanges(ctx context.Context, id ID) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.RequestChanges(ctx)
	})
}

// BeginApply moves awaiting-approval -> applying.
func (s *Supervisor) BeginApply(ctx context.Context, id ID) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.BeginApply(ctx)
	})
}

// TakeCheckpoint records a checkpoint.
func (s *Supervisor) TakeCheckpoint(ctx context.Context, id ID, ref string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.TakeCheckpoint(ctx, ref)
	})
}

// ReportError records a non-terminal error.
func (s *Supervisor) ReportError(ctx context.Context, id ID, msg string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.ReportError(ctx, msg)
	})
}

// Succeed moves to done.
func (s *Supervisor) Succeed(ctx context.Context, id ID) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.Succeed(ctx)
	})
}

// Fail moves to failed.
func (s *Supervisor) Fail(ctx context.Context, id ID, msg string) error {
	return s.do(ctx, id, func() error {
		m, err := s.machine(id)
		if err != nil {
			return err
		}
		return m.Fail(ctx, msg)
	})
}
