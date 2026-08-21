package session

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Log is an append-only session event log. The stream is a live tail of
// these rows. A later durable backend (the store port) can implement this
// without changing the machine.
type Log interface {
	Append(ctx context.Context, id ID, typ EventType, payload string) (Event, error)
	After(ctx context.Context, id ID, seq int64) ([]Event, error)
	ReplayLast(ctx context.Context, id ID, n int) ([]Event, error)
	SessionIDs(ctx context.Context) ([]ID, error)
	Subscribe(id ID) (wait func(context.Context) error, unsub func())
}

// MemoryLog is an in-memory [Log]. Stage 1 tests and Restore use it as the
// stand-in until the store port persists events. It is not a second source
// of truth beside the machine: the machine has no status of its own.
type MemoryLog struct {
	mu      sync.Mutex
	nextSeq int64
	events  map[string][]Event
	order   []ID
	wake    map[string]map[chan struct{}]struct{}
	now     func() time.Time
}

// NewMemoryLog returns an empty log.
func NewMemoryLog() *MemoryLog {
	return &MemoryLog{
		events: make(map[string][]Event),
		wake:   make(map[string]map[chan struct{}]struct{}),
		now:    func() time.Time { return time.Unix(0, time.Now().UTC().UnixNano()).UTC() },
	}
}

// NewMemoryLogFrom reconstructs a log from previously recorded events.
// Seq and timestamps are preserved so a daemon restart replays identically.
func NewMemoryLogFrom(events []Event) (*MemoryLog, error) {
	l := NewMemoryLog()
	var maxSeq int64
	for i, ev := range events {
		if ev.SessionID.IsZero() {
			return nil, fmt.Errorf("session log: event %d: empty id", i)
		}
		if ev.Type == "" {
			return nil, fmt.Errorf("session log: event %d: empty type", i)
		}
		key := ev.SessionID.String()
		if _, ok := l.events[key]; !ok {
			l.order = append(l.order, ev.SessionID)
			l.events[key] = nil
		}
		l.events[key] = append(l.events[key], ev)
		if ev.Seq > maxSeq {
			maxSeq = ev.Seq
		}
	}
	l.nextSeq = maxSeq
	return l, nil
}

// Snapshot returns every event across all sessions, ordered by seq.
func (l *MemoryLog) Snapshot() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, evs := range l.events {
		n += len(evs)
	}
	out := make([]Event, 0, n)
	for _, id := range l.order {
		out = append(out, l.events[id.String()]...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// Append implements [Log].
func (l *MemoryLog) Append(ctx context.Context, id ID, typ EventType, payload string) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, fmt.Errorf("session log append: %w", err)
	}
	if id.IsZero() {
		return Event{}, fmt.Errorf("session log append: empty id")
	}
	if typ == "" {
		return Event{}, fmt.Errorf("session log append: empty type")
	}
	l.mu.Lock()
	key := id.String()
	if _, ok := l.events[key]; !ok {
		l.order = append(l.order, id)
		l.events[key] = nil
	}
	l.nextSeq++
	ev := Event{
		Seq:       l.nextSeq,
		SessionID: id,
		Type:      typ,
		Payload:   payload,
		At:        l.now(),
	}
	l.events[key] = append(l.events[key], ev)
	l.mu.Unlock()
	l.broadcast(id)
	return ev, nil
}

// After implements [Log].
func (l *MemoryLog) After(ctx context.Context, id ID, seq int64) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("session log after: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	src := l.events[id.String()]
	out := make([]Event, 0, len(src))
	for _, ev := range src {
		if ev.Seq > seq {
			out = append(out, ev)
		}
	}
	return out, nil
}

// ReplayLast implements [Log]. Events are chronological.
func (l *MemoryLog) ReplayLast(ctx context.Context, id ID, n int) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("session log replay: %w", err)
	}
	if n < 1 {
		return nil, fmt.Errorf("session log replay: last %d", n)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	src := l.events[id.String()]
	if len(src) == 0 {
		return []Event{}, nil
	}
	start := len(src) - n
	if start < 0 {
		start = 0
	}
	out := make([]Event, len(src)-start)
	copy(out, src[start:])
	return out, nil
}

// SessionIDs implements [Log].
func (l *MemoryLog) SessionIDs(ctx context.Context) ([]ID, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("session log ids: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]ID, len(l.order))
	copy(out, l.order)
	return out, nil
}

// Subscribe implements [Log]. The channel is a wakeup, not a second log.
// Tails must still read rows via After.
func (l *MemoryLog) Subscribe(id ID) (wait func(context.Context) error, unsub func()) {
	ch := make(chan struct{}, 1)
	key := id.String()
	l.mu.Lock()
	if l.wake[key] == nil {
		l.wake[key] = make(map[chan struct{}]struct{})
	}
	l.wake[key][ch] = struct{}{}
	l.mu.Unlock()

	unsub = func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if subs, ok := l.wake[key]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(l.wake, key)
			}
		}
	}
	wait = func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			return nil
		}
	}
	return wait, unsub
}

func (l *MemoryLog) broadcast(id ID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ch := range l.wake[id.String()] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

var _ Log = (*MemoryLog)(nil)
