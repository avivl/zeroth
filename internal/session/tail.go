package session

import (
	"context"
	"fmt"
)

// Tail replays the last n events from the log, then yields live events as
// they are appended. Subscribe is a wakeup only; every live event is a row
// read back from the log, so attach cannot diverge from history.
func Tail(ctx context.Context, log Log, id ID, last int) (replay []Event, live <-chan Event, stop func(), err error) {
	if log == nil {
		return nil, nil, nil, fmt.Errorf("session tail: nil log")
	}
	if id.IsZero() {
		return nil, nil, nil, fmt.Errorf("session tail: empty id")
	}
	if last < 1 {
		return nil, nil, nil, fmt.Errorf("session tail: last %d", last)
	}

	wait, unsub := log.Subscribe(id)
	replay, err = log.ReplayLast(ctx, id, last)
	if err != nil {
		unsub()
		return nil, nil, nil, fmt.Errorf("session tail: %w", err)
	}

	var seq int64
	if n := len(replay); n > 0 {
		seq = replay[n-1].Seq
	}

	out := make(chan Event, 16)
	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(out)
		defer unsub()
		for {
			more, err := log.After(runCtx, id, seq)
			if err != nil {
				return
			}
			if len(more) > 0 {
				for _, ev := range more {
					select {
					case <-runCtx.Done():
						return
					case out <- ev:
						seq = ev.Seq
					}
				}
				continue
			}
			if err := wait(runCtx); err != nil {
				return
			}
		}
	}()
	stop = func() {
		cancel()
	}
	return replay, out, stop, nil
}
