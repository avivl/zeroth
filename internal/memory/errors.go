package memory

import "errors"

var (
	// ErrAgentCannotWrite is returned when an agent Actor tries to Write
	// or Delete. Agents propose; they do not touch the notebook.
	ErrAgentCannotWrite = errors.New("memory: agents cannot write the notebook")
	// ErrHumanCannotPropose is returned when a human Actor tries to Propose.
	// Humans write; the queue is the agent path.
	ErrHumanCannotPropose = errors.New("memory: humans write; they do not propose")
	// ErrNotPending is returned when Accept or Reject names a proposal
	// that is already reviewed.
	ErrNotPending = errors.New("memory: proposal is not pending")
	// ErrNotFound is returned when a key or proposal id is missing.
	ErrNotFound = errors.New("memory: not found")
	// ErrInvalid is returned for empty keys, bodies, actors, or ids.
	ErrInvalid = errors.New("memory: invalid")
)
