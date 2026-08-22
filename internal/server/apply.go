package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"go.uber.org/zap"
)

const applyPrincipal policy.PrincipalID = "operator"

// ApplyPublisher turns applied workspace files into a git branch and pull
// request. Tests inject a fake. The daemon default is git plus gh.
type ApplyPublisher interface {
	Publish(ctx context.Context, req ApplyPublish) (ApplyRef, error)
}

// ApplyPublish is one applied plan ready to land on git.
type ApplyPublish struct {
	Repo      string
	Workspace string
	Targets   []string
	Branch    string
	Title     string
	Body      string
	IssueKey  string
	PlanHash  string
}

// ApplyRef is the git/PR result of a successful publish.
type ApplyRef struct {
	Branch      string
	Commit      string
	PullRequest string
}

func (s *Server) applyApproved(ctx context.Context, sess store.Session, p plan.Plan) (plan.Result, string, error) {
	sid, err := session.ParseID(sess.ID.String())
	if err != nil {
		return plan.Result{}, "", fmt.Errorf("server apply: %w", err)
	}
	root, err := s.applyWorkspace(ctx, sid, sess)
	if err != nil {
		return plan.Result{}, "", err
	}
	world := newApplyWorld(root)
	aud := &applyAuditor{log: s.audit, agent: sess.AgentID, session: sess.ID, planID: p.ID.String(), hash: string(p.Hash)}
	applier := &plan.Applier{
		Kernel:      policy.New(),
		World:       world,
		Leases:      &applyLeaser{store: s.store, agent: sess.AgentID},
		Checkpoints: &applyCheckpointer{server: s, id: sid},
		Audit:       aud,
		Memory:      s.memoryQueue(sess),
	}
	result, err := applier.Apply(ctx, applyPrincipal, p, plan.Approval{PlanHash: p.Hash})
	if err != nil {
		return result, "", err
	}
	if result.Status == plan.StatusApplied {
		if err := s.publishApplied(ctx, sess, sid, p, world, root); err != nil {
			return result, "", err
		}
	}
	return result, aud.lastID, nil
}

func (s *Server) applyWorkspace(ctx context.Context, id session.ID, sess store.Session) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.sandbox != nil {
		s.mu.Lock()
		sbx := s.sandboxes[id.String()]
		s.mu.Unlock()
		dir, err := s.hostWorkspace(id, sbx)
		if err != nil {
			return "", fmt.Errorf("server apply workspace: %w", err)
		}
		return dir, nil
	}
	src := s.overlaySource(sess)
	if src == "" {
		return "", fmt.Errorf("server apply workspace: no sandbox overlay and no host checkout")
	}
	return src, nil
}

func (s *Server) publishApplied(ctx context.Context, sess store.Session, sid session.ID, p plan.Plan, world *applyWorld, root string) error {
	targets := world.targets()
	if len(targets) == 0 {
		return nil
	}
	issue := s.trackerKey(sid)
	req := ApplyPublish{
		Repo:      s.overlaySource(sess),
		Workspace: root,
		Targets:   targets,
		Branch:    gitBranchName(issue, p.Summary),
		Title:     strings.TrimSpace(p.Summary),
		Body:      applyPRBody(p, issue),
		IssueKey:  issue,
		PlanHash:  string(p.Hash),
	}
	if req.Title == "" {
		req.Title = "Apply approved plan"
	}
	ref, err := s.publisher.Publish(ctx, req)
	if err != nil {
		return fmt.Errorf("server apply publish: %w", err)
	}
	if u := strings.TrimSpace(ref.PullRequest); u != "" {
		s.rememberPR(sess.ID.String(), u)
	}
	s.log.Info("apply opened pull request",
		zap.String("run", sid.String()),
		zap.String("branch", ref.Branch),
		zap.String("commit", ref.Commit),
		zap.String("pr", ref.PullRequest),
	)
	return nil
}

func (s *Server) rememberPR(sessionID, url string) {
	url = strings.TrimSpace(url)
	if sessionID == "" || url == "" {
		return
	}
	s.mu.Lock()
	s.prs[sessionID] = url
	s.mu.Unlock()
}

func (s *Server) takePR(sessionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	url := s.prs[sessionID]
	delete(s.prs, sessionID)
	return url
}

type applyWorld struct {
	root    string
	mu      sync.Mutex
	applied map[string]string
	wrote   []string
}

func newApplyWorld(root string) *applyWorld {
	return &applyWorld{root: root, applied: make(map[string]string)}
}

func (w *applyWorld) Observe(_ context.Context, target string) (string, error) {
	path, err := safeJoin(w.root, target)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("server apply observe %s: %w", target, err)
	}
	return contentHash(body), nil
}

func (w *applyWorld) Execute(_ context.Context, row plan.Row) (string, error) {
	w.mu.Lock()
	if post, ok := w.applied[row.IdempotencyKey]; ok {
		w.mu.Unlock()
		return post, nil
	}
	w.mu.Unlock()

	post, err := applyRow(w.root, row)
	if err != nil {
		return "", err
	}
	w.mu.Lock()
	w.applied[row.IdempotencyKey] = post
	if !skipGitTarget(row.Target) {
		w.wrote = append(w.wrote, row.Target)
	}
	w.mu.Unlock()
	return post, nil
}

func (w *applyWorld) Seen(key string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	post, ok := w.applied[key]
	return post, ok
}

func (w *applyWorld) targets() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.wrote))
	copy(out, w.wrote)
	return out
}

func applyRow(root string, row plan.Row) (string, error) {
	path, err := safeJoin(root, row.Target)
	if err != nil {
		return "", err
	}
	switch row.Op {
	case plan.OpDestroy:
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("server apply destroy %s: %w", row.Target, err)
		}
		post := plan.EmptyDigest()
		if err := matchPostcondition(row, post); err != nil {
			return "", err
		}
		return post, nil
	case plan.OpCreate, plan.OpModify:
		body, err := materializePayload(path, row)
		if err != nil {
			return "", fmt.Errorf("server apply %s %s: %w", row.Op, row.Target, err)
		}
		post := plan.Digest(body)
		if err := matchPostcondition(row, post); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("server apply %s %s: %w", row.Op, row.Target, err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return "", fmt.Errorf("server apply %s %s: %w", row.Op, row.Target, err)
		}
		return post, nil
	default:
		return "", fmt.Errorf("server apply: unsupported op %s on %s", row.Op, row.Target)
	}
}

func materializePayload(path string, row plan.Row) ([]byte, error) {
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if os.IsNotExist(err) {
		current = nil
	}
	return plan.Materialize(row.Op, current, row.Payload)
}

func matchPostcondition(row plan.Row, post string) error {
	if row.Postcondition == "" || post == row.Postcondition {
		return nil
	}
	return fmt.Errorf("%w on %s: got %s", plan.ErrPostcondition, row.Target, post)
}

func contentHash(body []byte) string {
	return plan.Digest(body)
}

func emptyDigest() string {
	return plan.EmptyDigest()
}

func safeJoin(root, target string) (string, error) {
	rel := observeKey(target)
	rel = filepath.FromSlash(rel)
	if rel == "" || filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("server apply: unsafe target %q", target)
	}
	full := filepath.Join(root, rel)
	if !withinDir(root, full) {
		return "", fmt.Errorf("server apply: unsafe target %q", target)
	}
	return full, nil
}

func skipGitTarget(target string) bool {
	rel := observeKey(target)
	for _, p := range memory.CompiledPaths() {
		if rel == p {
			return true
		}
	}
	return false
}

func applyPRBody(p plan.Plan, issueKey string) string {
	var b strings.Builder
	b.WriteString("Approved Zeroth plan")
	if p.Hash != "" {
		fmt.Fprintf(&b, " `%s`", p.Hash)
	}
	b.WriteString(".\n")
	if issueKey != "" {
		fmt.Fprintf(&b, "\nSource issue: %s\n", issueKey)
	}
	if s := strings.TrimSpace(p.Summary); s != "" {
		fmt.Fprintf(&b, "\n%s\n", s)
	}
	b.WriteString("\nThis PR contains exactly the effects from the approved plan.\n")
	return b.String()
}

type applyLeaser struct {
	store store.Store
	agent store.AgentID
}

func (l *applyLeaser) Acquire(ctx context.Context, p plan.Plan) ([]policy.Lease, error) {
	kinds := make([]policy.EffectKind, 0, 4)
	seenKind := make(map[policy.EffectKind]struct{})
	for _, row := range p.Rows {
		k := row.Op.Kind()
		if _, ok := seenKind[k]; ok {
			continue
		}
		seenKind[k] = struct{}{}
		kinds = append(kinds, k)
	}
	grant, err := newGrantID()
	if err != nil {
		return nil, fmt.Errorf("server apply lease: %w", err)
	}
	scope, err := store.ParseScopeID(string(p.Scope))
	if err != nil {
		return nil, fmt.Errorf("server apply lease: %w", err)
	}
	now := time.Now().UTC()
	seen := make(map[string]struct{})
	out := make([]policy.Lease, 0)
	for _, row := range p.Rows {
		id := string(row.Lease)
		if id == "" {
			return nil, fmt.Errorf("server apply lease: %w", plan.ErrInvalid)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		lid, err := store.ParseLeaseID(id)
		if err != nil {
			return nil, fmt.Errorf("server apply lease: %w", err)
		}
		if err := l.store.CreateLease(ctx, store.Lease{
			ID:        lid,
			GrantID:   grant,
			ScopeID:   scope,
			AgentID:   l.agent,
			ExpiresAt: p.ExpiresAt,
			MintedAt:  now,
		}); err != nil && !errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("server apply lease: %w", err)
		}
		out = append(out, policy.NewLease(policy.LeaseID(id), p.Scope, applyPrincipal, p.ExpiresAt, kinds...))
	}
	return out, nil
}

func (l *applyLeaser) Release(_ context.Context, _ []policy.Lease) error {
	return nil
}

type applyCheckpointer struct {
	server *Server
	id     session.ID
}

func (c *applyCheckpointer) Checkpoint(ctx context.Context) (plan.CheckpointRef, error) {
	ck, err := c.server.snapshotRun(ctx, c.id, "pre-apply")
	if err != nil {
		return "", err
	}
	return plan.CheckpointRef(ck.ID.String()), nil
}

type applyAuditor struct {
	log     *audit.Log
	agent   store.AgentID
	session store.SessionID
	planID  string
	hash    string
	lastID  string
}

func (a *applyAuditor) SignRow(context.Context, plan.Row, string) error { return nil }

func (a *applyAuditor) SignPlan(ctx context.Context, p plan.Plan) error {
	rec, err := a.log.Append(ctx, audit.Entry{
		Action:        audit.ActionPlanApply,
		Target:        a.planID,
		PlanHash:      a.hash,
		Postcondition: string(p.Status),
		Approver:      audit.ApproverOperator,
		AgentID:       a.agent,
		SessionID:     a.session,
		ResourceType:  "plan",
		ResourceID:    a.planID,
	})
	if err != nil {
		return err
	}
	a.lastID = rec.ID.String()
	return nil
}
