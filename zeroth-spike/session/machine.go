package session

import (
	"fmt"
	"sync"
	"time"
)

// Status is a session lifecycle state.
type Status string

const (
	// StatusNew is a session that has not started.
	StatusNew Status = "new"
	// StatusRunning is a live session.
	StatusRunning Status = "running"
	// StatusStopped is a session that has ended. It does not restart.
	StatusStopped Status = "stopped"
)

// EventType names a recorded transition.
type EventType string

const (
	// EventCreated is appended when the machine is constructed.
	EventCreated EventType = "created"
	// EventStarted is appended on a successful Start.
	EventStarted EventType = "started"
	// EventStopped is appended on a successful Stop.
	EventStopped EventType = "stopped"
)

// Event is one append-only log record.
type Event struct {
	Type EventType
	At   time.Time
}

// Machine is a session state machine with an append-only event log.
type Machine struct {
	mu     sync.Mutex
	id     ID
	status Status
	events []Event
	now    func() time.Time
}

// New returns a session in StatusNew with a created event.
func New(id ID) (*Machine, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("session new: empty id")
	}
	m := &Machine{
		id:     id,
		status: StatusNew,
		now:    time.Now,
	}
	m.events = []Event{{Type: EventCreated, At: m.now()}}
	return m, nil
}

// ID returns the session identifier.
func (m *Machine) ID() ID {
	return m.id
}

// Status returns the current lifecycle state.
func (m *Machine) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Start moves new -> running and appends EventStarted.
func (m *Machine) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status != StatusNew {
		return fmt.Errorf("session start: status %s", m.status)
	}
	m.status = StatusRunning
	m.events = append(m.events, Event{Type: EventStarted, At: m.now()})
	return nil
}

// Stop moves running -> stopped and appends EventStopped.
func (m *Machine) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status != StatusRunning {
		return fmt.Errorf("session stop: status %s", m.status)
	}
	m.status = StatusStopped
	m.events = append(m.events, Event{Type: EventStopped, At: m.now()})
	return nil
}

// Events returns a copy of the append-only log.
func (m *Machine) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}
