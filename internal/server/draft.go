package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/tracker"
	"go.uber.org/zap"
)

// hostOverlay is implemented by sandbox drivers that can point the
// harness at the overlay's host directory. Docker does this. Drivers
// that do not implement it get a fresh temp dir.
type hostOverlay interface {
	HostWorkspace(id sandbox.ID) (string, error)
}

func (s *Server) prepareWorkspace(ctx context.Context, id session.ID) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	s.mu.Lock()
	sbx := s.sandboxes[id.String()]
	s.mu.Unlock()
	if s.sandbox != nil && !sbx.IsZero() {
		if hw, ok := s.sandbox.(hostOverlay); ok {
			dir, err := hw.HostWorkspace(sbx)
			if err == nil && strings.TrimSpace(dir) != "" {
				return dir, func() {}, nil
			}
			if err != nil {
				s.log.Debug("sandbox host workspace", zap.String("run", id.String()), zap.Error(err))
			}
		}
	}
	dir, err := os.MkdirTemp("", "zeroth-harness-")
	if err != nil {
		return "", nil, fmt.Errorf("server harness workspace: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func (s *Server) draftFromEffects(ctx context.Context, id session.ID, workspace string, effects []harness.Effect) error {
	if len(effects) == 0 {
		return fmt.Errorf("harness proposed empty effects")
	}
	proposed := make([]plan.Proposed, 0, len(effects))
	for _, e := range effects {
		proposed = append(proposed, plan.Proposed{Type: e.Type, Path: e.Path, Diff: e.Diff})
	}
	pid, err := plan.NewID()
	if err != nil {
		return fmt.Errorf("plan id: %w", err)
	}
	leaseRaw, err := newPrefixedID("lease_")
	if err != nil {
		return fmt.Errorf("lease id: %w", err)
	}
	now := time.Now().UTC()
	prompt := s.promptOf(ctx, id)
	key := s.trackerKey(id)
	built, err := plan.Build(plan.Draft{
		ID:        pid,
		SessionID: id,
		Summary:   planSummary(prompt, key),
		Effects:   proposed,
		Observed:  observeWorkspace(workspace, proposed),
		Lease:     policy.LeaseID(leaseRaw),
		ExpiresAt: now.Add(24 * time.Hour),
		Scope:     draftScope,
		Now:       now,
	})
	if err != nil {
		return fmt.Errorf("plan builder: %w", err)
	}
	rec, err := built.Record()
	if err != nil {
		return fmt.Errorf("plan record: %w", err)
	}
	if err := s.store.CreatePlan(ctx, rec); err != nil {
		return fmt.Errorf("store plan: %w", err)
	}
	if err := s.attachPlan(ctx, id, rec.ID, now); err != nil {
		s.log.Warn("attach plan to session", zap.String("run", id.String()), zap.Error(err))
	}
	if err := s.sup.ProposePlan(ctx, id, rec.ID.String()); err != nil {
		return fmt.Errorf("session plan: %w", err)
	}
	s.log.Info("plan drafted",
		zap.String("run", id.String()),
		zap.String("plan", rec.ID.String()),
		zap.Int("effects", len(proposed)),
	)

	out, err := s.ExamineDraft(ctx, rec.ID)
	if err != nil {
		return fmt.Errorf("cross-exam: %w", err)
	}
	s.log.Info("cross-exam finished",
		zap.String("run", id.String()),
		zap.String("plan", rec.ID.String()),
		zap.String("verdict", out.Exam.Verdict),
		zap.Bool("returned", out.Returned),
	)
	if s.tracker != nil && key != "" && !out.Returned {
		body := tracker.FormatPlanComment(string(out.Plan.Hash), out.Plan.Summary, out.Plan.Render())
		if _, err := s.tracker.Comment(ctx, key, body); err != nil {
			s.log.Warn("tracker plan comment", zap.String("key", key), zap.Error(err))
		}
	}
	if err := s.syncSession(ctx, id); err != nil {
		s.log.Warn("draft sync", zap.String("run", id.String()), zap.Error(err))
	}
	if out.Returned {
		notes := strings.TrimSpace(out.Exam.Reasoning)
		if notes == "" {
			notes = "cross-exam returned the plan to the agent"
		}
		return fmt.Errorf("%s", notes)
	}
	return nil
}

func (s *Server) attachPlan(ctx context.Context, id session.ID, planID store.PlanID, now time.Time) error {
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return err
	}
	sess, err := s.store.GetSession(ctx, sid)
	if err != nil {
		return err
	}
	sess.PlanID = planID
	sess.UpdatedAt = now
	return s.store.UpdateSession(ctx, sess)
}

func planSummary(prompt, key string) string {
	line := prompt
	if i := strings.IndexByte(prompt, '\n'); i >= 0 {
		line = prompt[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		if key != "" {
			return "proposed changes for " + key
		}
		return "proposed changes"
	}
	const max = 200
	if utf8.RuneCountInString(line) > max {
		runes := []rune(line)
		line = string(runes[:max])
	}
	return line
}

func observeWorkspace(root string, effects []plan.Proposed) map[string]string {
	out := make(map[string]string, len(effects))
	if strings.TrimSpace(root) == "" {
		return out
	}
	for _, e := range effects {
		rel := filepath.FromSlash(e.Path)
		if rel == "" || filepath.IsAbs(rel) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(body)
		out[e.Path] = hex.EncodeToString(sum[:])
	}
	return out
}
