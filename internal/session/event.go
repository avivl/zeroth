package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Status is a session lifecycle state. Attachment is a separate type.
type Status string

const (
	// StatusPending is a session that has been created and not started.
	StatusPending Status = "pending"
	// StatusRunning is a live session whose agent may emit work.
	StatusRunning Status = "running"
	// StatusAwaitingApproval is a live session blocked on a human plan gate.
	StatusAwaitingApproval Status = "awaiting-approval"
	// StatusApplying is a live session applying an approved plan.
	StatusApplying Status = "applying"
	// StatusDone is a successful terminal state. It does not restart.
	StatusDone Status = "done"
	// StatusFailed is a failed terminal state. It does not restart.
	StatusFailed Status = "failed"
)

// Terminal reports whether s is done or failed.
func (s Status) Terminal() bool {
	return s == StatusDone || s == StatusFailed
}

// Attachment is orthogonal to [Status]. Promotion and demotion change
// this, not the lifecycle.
type Attachment string

const (
	// AttachmentAttached means an operator is on the session.
	AttachmentAttached Attachment = "attached"
	// AttachmentBackground means the session keeps working without a
	// foreground operator. The completion contract applies.
	AttachmentBackground Attachment = "background"
)

// EventType names a recorded log row.
type EventType string

const (
	// EventCreated is appended when the machine is constructed.
	EventCreated EventType = "created"
	// EventStarted is appended on a successful Start (pending -> running).
	EventStarted EventType = "started"
	// EventToken is one agent output chunk.
	EventToken EventType = "token"
	// EventToolCall is one tool invocation the agent requested.
	EventToolCall EventType = "tool_call"
	// EventPlanProposed records a plan draft. Status stays running.
	EventPlanProposed EventType = "plan_proposed"
	// EventCrossExam records a reviewer verdict. Status stays running
	// until approval is requested or changes are returned to the agent.
	EventCrossExam EventType = "cross_exam"
	// EventApprovalRequested moves running -> awaiting-approval.
	EventApprovalRequested EventType = "approval_requested"
	// EventChangesRequested moves awaiting-approval -> running.
	// Payload is the operator comment the next draft must address.
	EventChangesRequested EventType = "changes_requested"
	// EventApplying moves awaiting-approval -> applying.
	EventApplying EventType = "applying"
	// EventCheckpointTaken records a checkpoint. Status is unchanged.
	EventCheckpointTaken EventType = "checkpoint_taken"
	// EventError records a non-terminal error. Status is unchanged.
	EventError EventType = "error"
	// EventSteered records operator guidance. Status is unchanged.
	EventSteered EventType = "steered"
	// EventBackgrounded demotes attached -> background.
	EventBackgrounded EventType = "backgrounded"
	// EventAttached promotes background -> attached.
	EventAttached EventType = "attached"
	// EventTerminal moves to done or failed. Payload is [PayloadDone] or [PayloadFailed].
	EventTerminal EventType = "terminal"
)

// PayloadDone and PayloadFailed are the only legal [EventTerminal] payloads.
const (
	PayloadDone   = "done"
	PayloadFailed = "failed"
)

// ErrIllegalTransition is returned (wrapped by [TransitionError]) when a
// proposed event is not legal in the current state.
var ErrIllegalTransition = errors.New("illegal transition")

// ErrNotFound is returned when a session id has no events.
var ErrNotFound = errors.New("session not found")

// TransitionError explains a denied transition. Unwraps to [ErrIllegalTransition].
type TransitionError struct {
	ID         ID
	Status     Status
	Attachment Attachment
	Type       EventType
}

func (e *TransitionError) Error() string {
	if e == nil {
		return "session: illegal transition"
	}
	id := e.ID.String()
	if id == "" {
		id = "<zero>"
	}
	if e.Status == "" {
		return fmt.Sprintf("session %s: illegal transition from empty state via %s", id, e.Type)
	}
	return fmt.Sprintf("session %s: illegal transition from %s (%s) via %s", id, e.Status, e.Attachment, e.Type)
}

func (e *TransitionError) Unwrap() error { return ErrIllegalTransition }

// CompletionContract is what a backgrounded session is allowed to do
// without a foreground operator. The default is finish the work, comment
// on the tracker issue, and ping the operator only on blockers.
type CompletionContract struct {
	Finish             bool `json:"finish"`
	CommentOnIssue     bool `json:"comment_on_issue"`
	PingOnlyOnBlockers bool `json:"ping_only_on_blockers"`
}

// DefaultCompletionContract is the demotion contract: finish, comment on
// the issue, ping only on blockers.
func DefaultCompletionContract() CompletionContract {
	return CompletionContract{
		Finish:             true,
		CommentOnIssue:     true,
		PingOnlyOnBlockers: true,
	}
}

// Event is one append-only log record. Seq is assigned by the log.
type Event struct {
	Seq       int64
	SessionID ID
	Type      EventType
	Payload   string
	At        time.Time
}

// State is the projection of a session log. It is derived, never stored
// as a second source of truth.
type State struct {
	ID         ID
	Status     Status
	Attachment Attachment
	Contract   CompletionContract
}

// Apply returns the state after ev, or [ErrIllegalTransition].
func Apply(st State, ev Event) (State, error) {
	if st.ID.IsZero() {
		if ev.Type != EventCreated {
			return State{}, &TransitionError{ID: ev.SessionID, Type: ev.Type}
		}
		if ev.SessionID.IsZero() {
			return State{}, fmt.Errorf("session apply: empty id")
		}
		return State{
			ID:         ev.SessionID,
			Status:     StatusPending,
			Attachment: AttachmentAttached,
		}, nil
	}
	if ev.SessionID.IsZero() {
		return State{}, fmt.Errorf("session apply: empty id")
	}
	if ev.SessionID != st.ID {
		return State{}, fmt.Errorf("session apply: id mismatch")
	}
	if err := checkPayload(ev); err != nil {
		return State{}, err
	}

	illegal := &TransitionError{
		ID:         st.ID,
		Status:     st.Status,
		Attachment: st.Attachment,
		Type:       ev.Type,
	}
	if st.Status.Terminal() {
		return State{}, illegal
	}

	out := st
	switch ev.Type {
	case EventCreated:
		return State{}, illegal
	case EventStarted:
		if st.Status != StatusPending {
			return State{}, illegal
		}
		out.Status = StatusRunning
	case EventApprovalRequested:
		if st.Status != StatusRunning {
			return State{}, illegal
		}
		out.Status = StatusAwaitingApproval
	case EventChangesRequested:
		if st.Status != StatusAwaitingApproval {
			return State{}, illegal
		}
		out.Status = StatusRunning
	case EventApplying:
		if st.Status != StatusAwaitingApproval {
			return State{}, illegal
		}
		out.Status = StatusApplying
	case EventTerminal:
		switch ev.Payload {
		case PayloadDone:
			if st.Status != StatusRunning && st.Status != StatusApplying {
				return State{}, illegal
			}
			out.Status = StatusDone
		case PayloadFailed:
			out.Status = StatusFailed
		default:
			return State{}, fmt.Errorf("session apply: terminal payload %q", ev.Payload)
		}
		out.Contract = CompletionContract{}
	case EventToken, EventToolCall:
		if st.Status != StatusRunning && st.Status != StatusApplying {
			return State{}, illegal
		}
	case EventPlanProposed, EventCrossExam:
		if st.Status != StatusRunning {
			return State{}, illegal
		}
	case EventCheckpointTaken, EventError:
		// Allowed in any non-terminal lifecycle. Already checked above.
	case EventSteered:
		if st.Status == StatusPending {
			return State{}, illegal
		}
	case EventBackgrounded:
		if st.Attachment != AttachmentAttached {
			return State{}, illegal
		}
		c, err := parseContract(ev.Payload)
		if err != nil {
			return State{}, err
		}
		out.Attachment = AttachmentBackground
		out.Contract = c
	case EventAttached:
		if st.Attachment != AttachmentBackground {
			return State{}, illegal
		}
		out.Attachment = AttachmentAttached
		out.Contract = CompletionContract{}
	default:
		return State{}, fmt.Errorf("session apply: unknown event %s", ev.Type)
	}
	return out, nil
}

func checkPayload(ev Event) error {
	switch ev.Type {
	case EventTerminal:
		if ev.Payload != PayloadDone && ev.Payload != PayloadFailed {
			return fmt.Errorf("session apply: terminal payload %q", ev.Payload)
		}
	case EventSteered, EventToolCall, EventError:
		if ev.Payload == "" {
			return fmt.Errorf("session apply: empty %s payload", ev.Type)
		}
	case EventBackgrounded:
		if ev.Payload == "" {
			return nil
		}
		if _, err := parseContract(ev.Payload); err != nil {
			return err
		}
	}
	return nil
}

func parseContract(payload string) (CompletionContract, error) {
	if payload == "" {
		return DefaultCompletionContract(), nil
	}
	var c CompletionContract
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		return CompletionContract{}, fmt.Errorf("session apply: contract: %w", err)
	}
	return c, nil
}

func encodeContract(c CompletionContract) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("session contract: %w", err)
	}
	return string(b), nil
}

// Replay folds events in order. An empty log is [ErrNotFound]. A single
// illegal event fails the whole replay: the log is the source of truth,
// so a corrupt log must not invent a state.
func Replay(events []Event) (State, error) {
	if len(events) == 0 {
		return State{}, fmt.Errorf("session replay: %w", ErrNotFound)
	}
	var st State
	for i, ev := range events {
		next, err := Apply(st, ev)
		if err != nil {
			return State{}, fmt.Errorf("session replay: event %d seq %d: %w", i, ev.Seq, err)
		}
		st = next
	}
	return st, nil
}
