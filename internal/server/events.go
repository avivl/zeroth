package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/avivl/zeroth/internal/session"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"go.uber.org/zap"
)

func (s *Server) GetRunEvents(w http.ResponseWriter, r *http.Request, id gen.RunID, params gen.GetRunEventsParams) {
	sid, err := session.ParseID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if _, err := s.sup.State(r.Context(), sid); err != nil {
		status, code, msg := statusForSessionError(err)
		writeError(w, status, code, msg)
		return
	}
	last, err := parseReplayLast(params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		s.streamWS(w, r, sid, last)
		return
	}
	replay, err := s.elog.ReplayLast(r.Context(), sid, last)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	items := make([]gen.RunEvent, 0, len(replay))
	for _, ev := range replay {
		items = append(items, apiEvent(ev))
	}
	writeJSON(w, http.StatusOK, gen.RunEventList{Items: items})
}

func (s *Server) streamWS(w http.ResponseWriter, r *http.Request, id session.ID, last int) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// CLI clients send no Origin. zerothd is loopback-only.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.log.Debug("websocket accept", zap.Error(err))
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	ctx := c.CloseRead(r.Context())
	if err := stream(ctx, s.elog, id, last, func(ev gen.RunEvent) error {
		return wsjson.Write(ctx, c, ev)
	}); err != nil && ctx.Err() == nil {
		s.log.Debug("websocket stream", zap.String("run", id.String()), zap.Error(err))
		_ = c.Close(websocket.StatusInternalError, "stream")
	}
}

// stream replays last N events from the log, then tails the same table.
// Every live frame is a stored row. There is no caught_up sentinel: the
// contract says each WebSocket message is a JSON RunEvent.
func stream(ctx context.Context, log session.Log, id session.ID, last int, emit func(gen.RunEvent) error) error {
	replay, live, stop, err := session.Tail(ctx, log, id, last)
	if err != nil {
		return err
	}
	defer stop()
	for _, ev := range replay {
		if err := emit(apiEvent(ev)); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-live:
			if !ok {
				return nil
			}
			if err := emit(apiEvent(ev)); err != nil {
				return err
			}
		}
	}
}
