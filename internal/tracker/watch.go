package tracker

import (
	"context"
	"fmt"
)

// Handler receives assign-to-Zeroth edges. The daemon implements this
// by starting a headless run (Assigned) and by failing that run and
// stopping its sandbox (Unassigned).
type Handler interface {
	OnAssigned(ctx context.Context, ev AssignmentEvent) error
	OnUnassigned(ctx context.Context, ev AssignmentEvent) error
}

// Watch reads [Provider.Assignments] until ctx is done and dispatches
// each event. It is the vendor-neutral assign-to-Zeroth loop.
func Watch(ctx context.Context, p Provider, h Handler) error {
	if p == nil {
		return fmt.Errorf("tracker watch: nil provider")
	}
	if h == nil {
		return fmt.Errorf("tracker watch: nil handler")
	}
	ch, err := p.Assignments(ctx)
	if err != nil {
		return fmt.Errorf("tracker watch: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := dispatch(ctx, h, ev); err != nil {
				return err
			}
		}
	}
}

func dispatch(ctx context.Context, h Handler, ev AssignmentEvent) error {
	switch ev.Kind {
	case Assigned:
		if err := h.OnAssigned(ctx, ev); err != nil {
			return fmt.Errorf("tracker watch assigned %s: %w", ev.Key, err)
		}
	case Unassigned:
		if err := h.OnUnassigned(ctx, ev); err != nil {
			return fmt.Errorf("tracker watch unassigned %s: %w", ev.Key, err)
		}
	default:
		return fmt.Errorf("tracker watch: unknown kind %q", ev.Kind)
	}
	return nil
}
