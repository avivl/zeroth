package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/tracker"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"go.uber.org/zap"
)

// errPRMerged is returned when retract cannot close a pull request
// because it already landed. Closing is the recovery; un-merge is not.
var errPRMerged = errors.New("pull request is already merged")

func (s *Server) RetractRun(w http.ResponseWriter, r *http.Request, id gen.RunID) {
	var req gen.RetractRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "reason is required")
		return
	}
	sid, err := session.ParseID(string(id))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.retractRun(r.Context(), sid, reason); err != nil {
		status, code, msg := statusForRetractError(err)
		writeError(w, status, code, msg)
		return
	}
	run, ok, err := s.loadRun(r.Context(), string(id))
	if err != nil || !ok {
		writeError(w, http.StatusInternalServerError, "internal", "run retracted but not readable")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) retractRun(ctx context.Context, id session.ID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("server retract: empty reason: %w", store.ErrInvalid)
	}
	storeID, err := store.ParseSessionID(id.String())
	if err != nil {
		return fmt.Errorf("server retract: %w", err)
	}
	s.sessMu.Lock()
	sess, err := s.store.GetSession(ctx, storeID)
	s.sessMu.Unlock()
	if err != nil {
		return fmt.Errorf("server retract: %w", err)
	}
	if !sess.RetractedAt.IsZero() {
		return fmt.Errorf("server retract: already retracted: %w", session.ErrIllegalTransition)
	}
	st, err := s.sup.State(ctx, id)
	if err != nil {
		return fmt.Errorf("server retract: %w", err)
	}
	if !st.Status.Terminal() {
		return fmt.Errorf("server retract: run is still live: %w", session.ErrIllegalTransition)
	}

	pr := s.peekPR(id.String())
	closed := false
	if pr != "" {
		if s.publisher == nil {
			return fmt.Errorf("server retract: no publisher to close %s", pr)
		}
		comment := retractPRComment(id.String(), reason)
		if err := s.publisher.ClosePullRequest(ctx, pr, comment); err != nil {
			return fmt.Errorf("server retract: %w", err)
		}
		closed = true
	}

	key := strings.TrimSpace(sess.TrackerRef)
	if key == "" {
		key = s.trackerKey(id)
	}
	if s.tracker != nil && key != "" {
		body := tracker.FormatRetractComment(tracker.Retract{
			RunID:       id.String(),
			Reason:      reason,
			PullRequest: pr,
			Closed:      closed || pr != "",
		})
		if _, err := s.tracker.Comment(ctx, key, body); err != nil {
			return fmt.Errorf("server retract comment: %w", err)
		}
		if err := s.tracker.SetState(ctx, key, tracker.State{Kind: tracker.StateUnstarted}); err != nil {
			s.log.Warn("retract set unstarted", zap.String("key", key), zap.Error(err))
		}
		s.forgetTracker(id, key)
		if err := s.tracker.Unassign(ctx, key); err != nil {
			return fmt.Errorf("server retract unassign: %w", err)
		}
	}

	now := time.Now().UTC()
	s.sessMu.Lock()
	sess, err = s.store.GetSession(ctx, storeID)
	if err != nil {
		s.sessMu.Unlock()
		return fmt.Errorf("server retract: %w", err)
	}
	sess.RetractReason = reason
	sess.RetractedAt = now
	sess.UpdatedAt = now
	if pr != "" {
		sess.PullRequest = pr
	}
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		s.sessMu.Unlock()
		return fmt.Errorf("server retract: %w", err)
	}
	s.sessMu.Unlock()

	if _, err := s.audit.Append(ctx, audit.Entry{
		Action:        audit.ActionRunRetract,
		Target:        firstNonEmpty(pr, id.String()),
		Postcondition: retractPostcondition(closed, pr),
		Approver:      audit.ApproverOperator,
		AgentID:       sess.AgentID,
		SessionID:     storeID,
		ResourceType:  "run",
		ResourceID:    id.String(),
	}); err != nil {
		return fmt.Errorf("server retract: %w", err)
	}
	s.log.Info("run retracted",
		zap.String("run", id.String()),
		zap.String("pr", pr),
		zap.String("tracker", key),
		zap.String("reason", reason),
	)
	return nil
}

func retractPRComment(runID, reason string) string {
	var b strings.Builder
	b.WriteString("Retracted by Zeroth")
	if runID != "" {
		fmt.Fprintf(&b, " (run `%s`)", runID)
	}
	b.WriteString(".\n\n")
	b.WriteString(strings.TrimSpace(reason))
	b.WriteString("\n")
	return b.String()
}

func retractPostcondition(closed bool, pr string) string {
	if pr == "" {
		return "retracted"
	}
	if closed {
		return "pr_closed"
	}
	return "retracted"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func statusForRetractError(err error) (int, string, string) {
	if errors.Is(err, errPRMerged) || strings.Contains(strings.ToLower(err.Error()), "already merged") {
		return http.StatusConflict, "conflict", err.Error()
	}
	return statusForSessionError(err)
}
