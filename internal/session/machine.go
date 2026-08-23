package session

import (
	"context"
	"fmt"
	"sync"
)

// Machine records transitions onto a [Log]. Status is always derived by
// replaying that log. The mutex only serializes check-then-append so two
// callers cannot both observe the same state and both succeed.
type Machine struct {
	mu  sync.Mutex
	id  ID
	log Log
}

// New appends EventCreated and returns a pending, attached session.
func New(ctx context.Context, id ID, log Log) (*Machine, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("session new: empty id")
	}
	if log == nil {
		return nil, fmt.Errorf("session new: nil log")
	}
	if _, err := log.Append(ctx, id, EventCreated, ""); err != nil {
		return nil, fmt.Errorf("session new: %w", err)
	}
	return &Machine{id: id, log: log}, nil
}

// Open returns a machine for an existing log. It does not append.
func Open(id ID, log Log) (*Machine, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("session open: empty id")
	}
	if log == nil {
		return nil, fmt.Errorf("session open: nil log")
	}
	return &Machine{id: id, log: log}, nil
}

// ID returns the session identifier.
func (m *Machine) ID() ID { return m.id }

// State replays the log. That is the only way to read lifecycle status.
func (m *Machine) State(ctx context.Context) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked(ctx)
}

func (m *Machine) stateLocked(ctx context.Context) (State, error) {
	evs, err := m.log.After(ctx, m.id, 0)
	if err != nil {
		return State{}, fmt.Errorf("session state: %w", err)
	}
	st, err := Replay(evs)
	if err != nil {
		return State{}, err
	}
	return st, nil
}

// Events returns a copy of this session's log, chronological.
func (m *Machine) Events(ctx context.Context) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	evs, err := m.log.After(ctx, m.id, 0)
	if err != nil {
		return nil, fmt.Errorf("session events: %w", err)
	}
	return evs, nil
}

func (m *Machine) record(ctx context.Context, typ EventType, payload string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stateLocked(ctx)
	if err != nil {
		return err
	}
	if _, err := Apply(st, Event{SessionID: m.id, Type: typ, Payload: payload}); err != nil {
		return err
	}
	if _, err := m.log.Append(ctx, m.id, typ, payload); err != nil {
		return fmt.Errorf("session %s: %w", m.id.String(), err)
	}
	return nil
}

// Start moves pending -> running.
func (m *Machine) Start(ctx context.Context) error {
	if err := m.record(ctx, EventStarted, ""); err != nil {
		return fmt.Errorf("session start: %w", err)
	}
	return nil
}

// EmitToken appends a token event.
func (m *Machine) EmitToken(ctx context.Context, tok string) error {
	if err := m.record(ctx, EventToken, tok); err != nil {
		return fmt.Errorf("session token: %w", err)
	}
	return nil
}

// EmitToolCall appends a tool-call event.
func (m *Machine) EmitToolCall(ctx context.Context, call string) error {
	if err := m.record(ctx, EventToolCall, call); err != nil {
		return fmt.Errorf("session tool call: %w", err)
	}
	return nil
}

// ProposePlan appends a plan-proposed event. Status stays running.
func (m *Machine) ProposePlan(ctx context.Context, ref string) error {
	if err := m.record(ctx, EventPlanProposed, ref); err != nil {
		return fmt.Errorf("session plan: %w", err)
	}
	return nil
}

// RecordCrossExam appends a reviewer verdict. Status stays running.
func (m *Machine) RecordCrossExam(ctx context.Context, payload string) error {
	if err := m.record(ctx, EventCrossExam, payload); err != nil {
		return fmt.Errorf("session cross-exam: %w", err)
	}
	return nil
}

// RequestApproval moves running -> awaiting-approval.
func (m *Machine) RequestApproval(ctx context.Context, ref string) error {
	if err := m.record(ctx, EventApprovalRequested, ref); err != nil {
		return fmt.Errorf("session approval: %w", err)
	}
	return nil
}

// RequestChanges moves awaiting-approval -> running.
func (m *Machine) RequestChanges(ctx context.Context, comment string) error {
	if err := m.record(ctx, EventChangesRequested, comment); err != nil {
		return fmt.Errorf("session changes: %w", err)
	}
	return nil
}

// BeginApply moves awaiting-approval -> applying.
func (m *Machine) BeginApply(ctx context.Context) error {
	if err := m.record(ctx, EventApplying, ""); err != nil {
		return fmt.Errorf("session apply: %w", err)
	}
	return nil
}

// TakeCheckpoint appends a checkpoint event.
func (m *Machine) TakeCheckpoint(ctx context.Context, ref string) error {
	if err := m.record(ctx, EventCheckpointTaken, ref); err != nil {
		return fmt.Errorf("session checkpoint: %w", err)
	}
	return nil
}

// ReportError appends a non-terminal error event.
func (m *Machine) ReportError(ctx context.Context, msg string) error {
	if err := m.record(ctx, EventError, msg); err != nil {
		return fmt.Errorf("session error: %w", err)
	}
	return nil
}

// Steer appends operator guidance. It does not skip plan-then-apply.
func (m *Machine) Steer(ctx context.Context, msg string) error {
	if err := m.record(ctx, EventSteered, msg); err != nil {
		return fmt.Errorf("session steer: %w", err)
	}
	return nil
}

// Background demotes attached -> background and records the completion
// contract. A nil contract uses [DefaultCompletionContract].
func (m *Machine) Background(ctx context.Context, contract *CompletionContract) error {
	c := DefaultCompletionContract()
	if contract != nil {
		c = *contract
	}
	payload, err := encodeContract(c)
	if err != nil {
		return fmt.Errorf("session background: %w", err)
	}
	if err := m.record(ctx, EventBackgrounded, payload); err != nil {
		return fmt.Errorf("session background: %w", err)
	}
	return nil
}

// Foreground promotes background -> attached and clears the contract.
func (m *Machine) Foreground(ctx context.Context) error {
	if err := m.record(ctx, EventAttached, ""); err != nil {
		return fmt.Errorf("session foreground: %w", err)
	}
	return nil
}

// Succeed moves running or applying -> done.
func (m *Machine) Succeed(ctx context.Context) error {
	if err := m.record(ctx, EventTerminal, PayloadDone); err != nil {
		return fmt.Errorf("session succeed: %w", err)
	}
	return nil
}

// Fail moves any non-terminal status -> failed.
func (m *Machine) Fail(ctx context.Context, msg string) error {
	// The terminal event is the transition. The message is recorded first
	// so replay still sees why, unless we are pending (error is legal there
	// too) or the extra error event would itself be illegal.
	if msg != "" {
		if err := m.record(ctx, EventError, msg); err != nil {
			return fmt.Errorf("session fail: %w", err)
		}
	}
	if err := m.record(ctx, EventTerminal, PayloadFailed); err != nil {
		return fmt.Errorf("session fail: %w", err)
	}
	return nil
}
