package spike

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/eventlog"
	"github.com/avivl/zeroth/zeroth-spike/session"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// FrameCaughtUp is sent after replay, before live tail. It is not stored.
const FrameCaughtUp = "caught_up"

// Frame is one WebSocket or HTTP event payload. Replay frames have Replay
// set so attach latency can ignore history.
type Frame struct {
	Seq       int64  `json:"seq,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Type      string `json:"type"`
	Payload   string `json:"payload,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Replay    bool   `json:"replay,omitempty"`
}

func frameFromEvent(ev eventlog.Event, replay bool) Frame {
	return Frame{
		Seq:       ev.Seq,
		SessionID: ev.SessionID.String(),
		Type:      ev.Type,
		Payload:   ev.Payload,
		CreatedAt: ev.CreatedAt.UTC().Format(time.RFC3339Nano),
		Replay:    replay,
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	last, err := parseLast(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	_, found, err := s.sup.Lookup(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		s.streamWS(w, r, id, last)
		return
	}
	replay, err := s.log.ReplayLast(r.Context(), id, last)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	items := make([]Frame, 0, len(replay))
	for _, ev := range replay {
		items = append(items, frameFromEvent(ev, true))
	}
	writeJSON(w, http.StatusOK, eventListResponse{Items: items})
}

func (s *Server) streamWS(w http.ResponseWriter, r *http.Request, id session.ID, last int) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// CLI clients send no Origin. This process is loopback-only.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	ctx := c.CloseRead(r.Context())
	if err := Stream(ctx, s.log, id, last, func(f Frame) error {
		return wsjson.Write(ctx, c, f)
	}); err != nil && ctx.Err() == nil {
		_ = c.Close(websocket.StatusInternalError, "stream")
	}
}

// Stream replays last N events from SQLite, emits caught_up, then tails
// the same table. Wakeups are not a second log; every live frame is a row.
func Stream(ctx context.Context, log *eventlog.Log, id session.ID, last int, emit func(Frame) error) error {
	wait, unsub := log.Subscribe(id)
	defer unsub()

	replay, err := log.ReplayLast(ctx, id, last)
	if err != nil {
		return fmt.Errorf("spike stream replay: %w", err)
	}
	var seq int64
	for _, ev := range replay {
		if err := emit(frameFromEvent(ev, true)); err != nil {
			return fmt.Errorf("spike stream replay emit: %w", err)
		}
		seq = ev.Seq
	}
	if err := emit(Frame{Type: FrameCaughtUp, Seq: seq}); err != nil {
		return fmt.Errorf("spike stream caught_up: %w", err)
	}

	for {
		more, err := log.After(ctx, id, seq)
		if err != nil {
			return fmt.Errorf("spike stream after: %w", err)
		}
		if len(more) > 0 {
			for _, ev := range more {
				if err := emit(frameFromEvent(ev, false)); err != nil {
					return fmt.Errorf("spike stream live emit: %w", err)
				}
				seq = ev.Seq
			}
			continue
		}
		poll, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
		err = wait(poll)
		cancel()
		if err == nil || err == context.DeadlineExceeded {
			continue
		}
		return err
	}
}
