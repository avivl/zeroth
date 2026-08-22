package server

import (
	"context"
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
// harness at the overlay's host directory. Docker does this. Stage 1
// runs the harness as a host subprocess against that path (ADR-Z-0010),
// so the plan builder and the agent see the same tree. When a sandbox
// is in play, missing this method is a hard error: falling back to an
// empty temp dir hides the real checkout and drops preconditions at
// draft time.
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
	if s.sandbox != nil {
		dir, err := s.hostWorkspace(id, sbx)
		if err != nil {
			return "", nil, err
		}
		s.log.Info("harness workspace is sandbox overlay",
			zap.String("run", id.String()),
			zap.String("dir", dir),
			zap.String("sandbox", s.sandbox.Name()),
		)
		return dir, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "zeroth-harness-")
	if err != nil {
		return "", nil, fmt.Errorf("server harness workspace: %w", err)
	}
	s.log.Warn("harness workspace using empty temp dir; no sandbox driver is configured",
		zap.String("run", id.String()),
		zap.String("dir", dir),
	)
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func (s *Server) hostWorkspace(id session.ID, sbx sandbox.ID) (string, error) {
	if sbx.IsZero() {
		return "", fmt.Errorf("server harness workspace: run %s has no sandbox record", id.String())
	}
	hw, ok := s.sandbox.(hostOverlay)
	if !ok {
		return "", fmt.Errorf("server harness workspace: sandbox %s does not expose a host workspace", s.sandbox.Name())
	}
	dir, err := hw.HostWorkspace(sbx)
	if err != nil {
		return "", fmt.Errorf("server harness workspace: sandbox %s host workspace: %w", s.sandbox.Name(), err)
	}
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("server harness workspace: sandbox %s returned an empty host workspace", s.sandbox.Name())
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("server harness workspace: %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("server harness workspace: %s is not a directory", dir)
	}
	return dir, nil
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
	observed, bodies, err := observeWorkspace(workspace, proposed)
	if err != nil {
		return err
	}
	built, err := plan.Build(plan.Draft{
		ID:        pid,
		SessionID: id,
		Summary:   planSummary(prompt, key),
		Effects:   proposed,
		Observed:  observed,
		Bodies:    bodies,
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
		return fmt.Errorf("attach plan to session: %w", err)
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
		return fmt.Errorf("server attach plan: %w", err)
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	sess, err := s.store.GetSession(ctx, sid)
	if err != nil {
		return fmt.Errorf("server attach plan: %w", err)
	}
	sess.PlanID = planID
	sess.UpdatedAt = now
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("server attach plan: %w", err)
	}
	return nil
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

func observeWorkspace(root string, effects []plan.Proposed) (map[string]string, map[string]string, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil, fmt.Errorf("could not observe workspace at draft time: empty workspace root")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, fmt.Errorf("could not observe workspace at draft time: %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("could not observe workspace at draft time: %s is not a directory", root)
	}
	out := make(map[string]string, len(effects))
	bodies := make(map[string]string, len(effects))
	for _, e := range effects {
		key := observeKey(e.Path)
		rel := filepath.FromSlash(key)
		if rel == "" || filepath.IsAbs(rel) {
			continue
		}
		path := filepath.Join(root, rel)
		body, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				if needsPrecondition(e.Type) {
					return nil, nil, fmt.Errorf("could not observe workspace at draft time: %s: file not found under %s", key, root)
				}
				continue
			}
			return nil, nil, fmt.Errorf("could not observe workspace at draft time: %s: %w", key, err)
		}
		out[key] = contentHash(body)
		bodies[key] = string(body)
	}
	return out, bodies, nil
}

func observeKey(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	return strings.TrimPrefix(p, "./")
}

func needsPrecondition(typ string) bool {
	op, ok := plan.ParseOp(typ)
	return ok && (op == plan.OpModify || op == plan.OpDestroy)
}
