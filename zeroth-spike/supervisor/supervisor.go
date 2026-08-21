package supervisor

import (
	"context"
	"fmt"
	"sync"

	"github.com/avivl/zeroth/zeroth-spike/eventlog"
	"github.com/avivl/zeroth/zeroth-spike/session"
)

// Supervisor owns running sessions and writes their events to the log.
type Supervisor struct {
	log  *eventlog.Log
	root context.Context
	stop context.CancelFunc

	mu   sync.Mutex
	runs map[session.ID]*run
}

type run struct {
	machine *session.Machine
	cancel  context.CancelFunc
	agent   string
	done    chan struct{}
}

// New returns a supervisor that writes to log.
func New(log *eventlog.Log) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{
		log:  log,
		root: ctx,
		stop: cancel,
		runs: make(map[session.ID]*run),
	}
}

// Close cancels every agent and waits for them to exit.
func (s *Supervisor) Close() {
	s.stop()
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

// Start creates a session, starts the agent, and returns the id.
func (s *Supervisor) Start(agent Agent) (session.ID, error) {
	if agent == nil {
		return session.ID{}, fmt.Errorf("supervisor start: nil agent")
	}
	id, err := session.NewID()
	if err != nil {
		return session.ID{}, fmt.Errorf("supervisor start: %w", err)
	}
	m, err := session.New(id)
	if err != nil {
		return session.ID{}, fmt.Errorf("supervisor start: %w", err)
	}
	ctx := s.root
	if err := s.log.CreateSession(ctx, id, string(session.StatusNew), true); err != nil {
		return session.ID{}, fmt.Errorf("supervisor start: %w", err)
	}
	if _, err := s.log.Append(ctx, id, eventlog.TypeCreated, ""); err != nil {
		return session.ID{}, fmt.Errorf("supervisor start: %w", err)
	}
	if err := m.Start(); err != nil {
		return session.ID{}, fmt.Errorf("supervisor start: %w", err)
	}
	if err := s.log.SetSession(ctx, id, string(session.StatusRunning), true); err != nil {
		return session.ID{}, fmt.Errorf("supervisor start: %w", err)
	}
	if _, err := s.log.Append(ctx, id, eventlog.TypeStarted, ""); err != nil {
		return session.ID{}, fmt.Errorf("supervisor start: %w", err)
	}

	runCtx, cancel := context.WithCancel(s.root)
	r := &run{
		machine: m,
		cancel:  cancel,
		agent:   agent.Name(),
		done:    make(chan struct{}),
	}
	s.mu.Lock()
	s.runs[id] = r
	s.mu.Unlock()

	go s.drive(runCtx, id, r, agent)
	return id, nil
}

func (s *Supervisor) drive(ctx context.Context, id session.ID, r *run, agent Agent) {
	defer close(r.done)
	emit := func(payload string) error {
		_, err := s.log.Append(ctx, id, eventlog.TypeToken, payload)
		if err != nil {
			return fmt.Errorf("supervisor emit: %w", err)
		}
		return nil
	}
	err := agent.Run(ctx, emit)
	// Context cancel is a normal stop. Other errors still end the session.
	_ = err
	s.finish(id, r)
}

func (s *Supervisor) finish(id session.ID, r *run) {
	s.mu.Lock()
	current, ok := s.runs[id]
	if !ok || current != r {
		s.mu.Unlock()
		return
	}
	delete(s.runs, id)
	s.mu.Unlock()

	if err := r.machine.Stop(); err != nil {
		return
	}
	ctx := context.Background()
	_ = s.log.SetSession(ctx, id, string(session.StatusStopped), false)
	_, _ = s.log.Append(ctx, id, eventlog.TypeStopped, "")
}

// Background demotes a running session. The agent keeps emitting.
func (s *Supervisor) Background(id session.ID) error {
	s.mu.Lock()
	r, ok := s.runs[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("supervisor bg: %s not running", id.String())
	}
	ctx := s.root
	if err := s.log.SetSession(ctx, id, string(r.machine.Status()), false); err != nil {
		return fmt.Errorf("supervisor bg: %w", err)
	}
	if _, err := s.log.Append(ctx, id, eventlog.TypeBackgrounded, ""); err != nil {
		return fmt.Errorf("supervisor bg: %w", err)
	}
	return nil
}

// Stop cancels the agent. The stopped event is written from drive.
func (s *Supervisor) Stop(id session.ID) error {
	s.mu.Lock()
	r, ok := s.runs[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("supervisor stop: %s not running", id.String())
	}
	r.cancel()
	<-r.done
	return nil
}

// Info is a snapshot for HTTP.
type Info struct {
	ID         session.ID
	Status     session.Status
	Foreground bool
	Agent      string
	Running    bool
}

// Lookup returns live supervisor state, falling back to the session row.
func (s *Supervisor) Lookup(ctx context.Context, id session.ID) (Info, bool, error) {
	s.mu.Lock()
	r, running := s.runs[id]
	agent := ""
	var status session.Status
	if running {
		agent = r.agent
		status = r.machine.Status()
	}
	s.mu.Unlock()

	row, found, err := s.log.GetSession(ctx, id)
	if err != nil {
		return Info{}, false, err
	}
	if !found && !running {
		return Info{}, false, nil
	}
	info := Info{
		ID:      id,
		Running: running,
		Agent:   agent,
	}
	if found {
		info.Status = session.Status(row.Status)
		info.Foreground = row.Foreground
	}
	if running {
		info.Status = status
	}
	return info, true, nil
}
