package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	defaultAttachLast = 50
	reconnectMin      = 50 * time.Millisecond
	reconnectMax      = 2 * time.Second
)

// followRunEvents replays last N events, then live-tails. On a dropped
// connection it redials and skips event ids already delivered so attach
// neither duplicates nor (within the replay window) loses frames.
func followRunEvents(ctx context.Context, httpOrigin, runID string, last int, lastSeen string, emit func(gen.RunEvent) error) error {
	if last < 1 {
		last = defaultAttachLast
	}
	backoff := reconnectMin
	for {
		err := followOnce(ctx, httpOrigin, runID, last, &lastSeen, emit)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return nil
		}
		if isGone(err) {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if backoff < reconnectMax {
			backoff *= 2
			if backoff > reconnectMax {
				backoff = reconnectMax
			}
		}
	}
}

func followOnce(ctx context.Context, httpOrigin, runID string, last int, lastSeen *string, emit func(gen.RunEvent) error) error {
	url := wsBase(httpOrigin) + "/runs/" + runID + "/events?last=" + strconv.Itoa(last)
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("zeroth attach: %w", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	for {
		var ev gen.RunEvent
		if err := wsjson.Read(ctx, c, &ev); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("zeroth attach: %w", err)
		}
		if !retainEvent(*lastSeen, ev.Id) {
			continue
		}
		*lastSeen = ev.Id
		if err := emit(ev); err != nil {
			return err
		}
		if isTerminalEvent(ev) {
			return nil
		}
	}
}

// retainEvent reports whether eventID should be delivered given lastSeen.
// Event ids are the store seq as decimal strings; comparison is numeric
// so reconnect can skip the replay window without duplicates.
func retainEvent(lastSeen, eventID string) bool {
	if lastSeen == "" || eventID == "" {
		return eventID != ""
	}
	a, aErr := strconv.ParseInt(lastSeen, 10, 64)
	b, bErr := strconv.ParseInt(eventID, 10, 64)
	if aErr != nil || bErr != nil {
		return eventID != lastSeen
	}
	return b > a
}

func isTerminalEvent(ev gen.RunEvent) bool {
	if ev.Type != "status_changed" || ev.Message == nil {
		return false
	}
	switch *ev.Message {
	case "completed", "failed", "cancelled", "stopped":
		return true
	default:
		return false
	}
}

func isGone(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

func formatEvent(ev gen.RunEvent) string {
	msg := ""
	if ev.Message != nil {
		msg = *ev.Message
	}
	return strings.TrimSpace(ev.Id + "  " + ev.Type + "  " + msg)
}

func scanLines(ctx context.Context, r io.Reader, fn func(string) error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		_ = fn(line)
	}
}
