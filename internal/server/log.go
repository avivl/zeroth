package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
)

// storeLog implements [session.Log] on a [store.Store]. Subscribe is an
// in-process wakeup; every live frame is still a row read back from the
// store, so attach cannot diverge from history.
type storeLog struct {
	store store.Store

	mu   sync.Mutex
	wake map[string]map[chan struct{}]struct{}
}

func newStoreLog(st store.Store) *storeLog {
	return &storeLog{
		store: st,
		wake:  make(map[string]map[chan struct{}]struct{}),
	}
}

func (l *storeLog) Append(ctx context.Context, id session.ID, typ session.EventType, payload string) (session.Event, error) {
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return session.Event{}, fmt.Errorf("server log append: %w", err)
	}
	ev, err := l.store.AppendEvent(ctx, sid, store.Event{
		Type:    string(typ),
		Payload: payload,
		Message: payload,
	})
	if err != nil {
		return session.Event{}, fmt.Errorf("server log append: %w", err)
	}
	out := sessionEvent(id, ev)
	l.broadcast(id)
	return out, nil
}

func (l *storeLog) After(ctx context.Context, id session.ID, seq int64) ([]session.Event, error) {
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return nil, fmt.Errorf("server log after: %w", err)
	}
	rows, err := l.store.EventsAfter(ctx, sid, seq)
	if err != nil {
		return nil, fmt.Errorf("server log after: %w", err)
	}
	return sessionEvents(id, rows), nil
}

func (l *storeLog) ReplayLast(ctx context.Context, id session.ID, n int) ([]session.Event, error) {
	if n < 1 {
		return nil, fmt.Errorf("server log replay: last %d", n)
	}
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return nil, fmt.Errorf("server log replay: %w", err)
	}
	rows, err := l.store.ReplayLast(ctx, sid, n)
	if err != nil {
		return nil, fmt.Errorf("server log replay: %w", err)
	}
	return sessionEvents(id, rows), nil
}

func (l *storeLog) SessionIDs(ctx context.Context) ([]session.ID, error) {
	var ids []session.ID
	var cursor string
	for {
		page, err := l.store.ListSessions(ctx, store.SessionQuery{
			PageQuery: store.PageQuery{Limit: store.MaxLimit, Cursor: cursor},
		})
		if err != nil {
			return nil, fmt.Errorf("server log ids: %w", err)
		}
		for _, sess := range page.Items {
			id, err := session.ParseID(sess.ID.String())
			if err != nil {
				return nil, fmt.Errorf("server log ids: %w", err)
			}
			ids = append(ids, id)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if ids == nil {
		ids = []session.ID{}
	}
	return ids, nil
}

func (l *storeLog) Subscribe(id session.ID) (wait func(context.Context) error, unsub func()) {
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

func (l *storeLog) broadcast(id session.ID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ch := range l.wake[id.String()] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func sessionEvent(id session.ID, ev store.Event) session.Event {
	return session.Event{
		Seq:       ev.Seq,
		SessionID: id,
		Type:      session.EventType(ev.Type),
		Payload:   ev.Payload,
		At:        ev.CreatedAt.UTC(),
	}
}

func sessionEvents(id session.ID, rows []store.Event) []session.Event {
	out := make([]session.Event, 0, len(rows))
	for _, ev := range rows {
		out = append(out, sessionEvent(id, ev))
	}
	return out
}

var _ session.Log = (*storeLog)(nil)
