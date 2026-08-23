package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
)

func TestApplyLeaserReleaseRevokesAuthorization(t *testing.T) {
	t.Parallel()
	st, leaser := newApplyLeaser(t)
	p := approvedLeasePlan(t, "lease-1")
	ctx := t.Context()

	held, err := leaser.Acquire(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 {
		t.Fatalf("acquired %d leases, want 1", len(held))
	}
	lid, err := store.ParseLeaseID(string(held[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetLease(ctx, lid); err != nil {
		t.Fatalf("store row after acquire: %v", err)
	}

	now := time.Now().UTC()
	effect := p.Rows[0].PolicyEffect(p.Scope)
	got := policy.New().Authorize(now, applyPrincipal, effect, held)
	if !got.Allowed {
		t.Fatalf("acquired lease should authorize: %s", got.Reason)
	}

	if err := leaser.Release(ctx, held); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetLease(ctx, lid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("store row after release: %v, want ErrNotFound", err)
	}

	// Deny by default: a missing store row is an empty lease set, not a
	// lingering grant. Re-using the in-memory copy would still look valid,
	// so the check that matters is the one against what the store still holds.
	live, err := livePolicyLeases(ctx, st, held)
	if err != nil {
		t.Fatal(err)
	}
	got = policy.New().Authorize(now, applyPrincipal, effect, live)
	if got.Allowed {
		t.Fatalf("released lease still authorized: %s", got.Reason)
	}
}

func TestApplyLeaserReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	_, leaser := newApplyLeaser(t)
	p := approvedLeasePlan(t, "lease-1")
	ctx := t.Context()
	held, err := leaser.Acquire(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := leaser.Release(ctx, held); err != nil {
		t.Fatal(err)
	}
	if err := leaser.Release(ctx, held); err != nil {
		t.Fatalf("second release: %v", err)
	}
}

func TestApplyLeaserAcquireCleansUpOnPartialFailure(t *testing.T) {
	t.Parallel()
	st, base := newApplyLeaser(t)
	failing := &createLeaseFailer{Store: st, failAt: 2}
	leaser := &applyLeaser{store: failing, agent: base.agent}
	p := approvedLeasePlan(t, "lease-1")
	p.Rows = append([]plan.Row(nil), p.Rows...)
	second := p.Rows[0]
	second.Target = "b.txt"
	second.Lease = "lease-2"
	second.IdempotencyKey = "idem-b"
	p.Rows = append(p.Rows, second)
	p.Hash = plan.HashOf(p)

	_, err := leaser.Acquire(t.Context(), p)
	if err == nil {
		t.Fatal("expected acquire to fail on the second insert")
	}
	for _, id := range []string{"lease-1", "lease-2"} {
		lid, parseErr := store.ParseLeaseID(id)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if _, getErr := st.GetLease(t.Context(), lid); !errors.Is(getErr, store.ErrNotFound) {
			t.Fatalf("%s still in store after failed acquire: %v", id, getErr)
		}
	}
}

func TestApplyReleasesLeaseOnEveryTerminationPath(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	cases := []struct {
		name  string
		world func(cancel context.CancelFunc) plan.Executor
		ok    bool
	}{
		{
			name:  "success",
			world: func(context.CancelFunc) plan.Executor { return newHashWorld("h1") },
			ok:    true,
		},
		{
			name: "failure",
			world: func(context.CancelFunc) plan.Executor {
				w := newHashWorld("h1")
				w.fail = errors.New("disk full")
				return w
			},
		},
		{
			name: "cancellation",
			world: func(cancel context.CancelFunc) plan.Executor {
				w := newHashWorld("h1")
				w.cancel = cancel
				return w
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st, leaser := newApplyLeaser(t)
			p := approvedLeasePlan(t, "lease-"+tc.name)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			applier := &plan.Applier{
				Kernel:      policy.New(),
				Clock:       staticClock{now: now},
				World:       tc.world(cancel),
				Leases:      leaser,
				Checkpoints: nopCK{},
				Audit:       nopAudit{},
			}
			_, err := applier.Apply(ctx, applyPrincipal, p, plan.Approval{PlanHash: p.Hash})
			if tc.ok {
				if err != nil {
					t.Fatalf("apply: %v", err)
				}
			} else if err == nil {
				t.Fatal("expected apply to fail")
			}
			lid, parseErr := store.ParseLeaseID(string(p.Rows[0].Lease))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if _, getErr := st.GetLease(t.Context(), lid); !errors.Is(getErr, store.ErrNotFound) {
				t.Fatalf("lease still in store after %s: %v", tc.name, getErr)
			}
		})
	}
}

func newApplyLeaser(t *testing.T) (store.Store, *applyLeaser) {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	aid, err := store.ParseAgentID("lease-agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAgent(t.Context(), store.Agent{
		ID: aid, Name: "lease-agent", Harness: "claudecode", Status: "ready",
	}); err != nil {
		t.Fatal(err)
	}
	return st, &applyLeaser{store: st, agent: aid}
}

func approvedLeasePlan(t *testing.T, leaseID string) plan.Plan {
	t.Helper()
	pid, err := plan.NewID()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	draft, err := plan.Build(plan.Draft{
		ID:        pid,
		SessionID: sid,
		Summary:   "lease release",
		Effects:   []plan.Proposed{{Type: "modify", Path: "a.txt", Diff: "A"}},
		Observed:  map[string]string{"a.txt": "h1"},
		Lease:     policy.LeaseID(leaseID),
		ExpiresAt: now.Add(time.Hour),
		Scope:     "scope-a",
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	draft.Status = plan.StatusPendingApproval
	draft.CrossExam = &plan.CrossExam{Verdict: plan.VerdictPass, ReviewerModel: "test-reviewer", At: now}
	approved, err := draft.Approve(now)
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func livePolicyLeases(ctx context.Context, st store.Store, held []policy.Lease) ([]policy.Lease, error) {
	out := make([]policy.Lease, 0, len(held))
	for _, pl := range held {
		lid, err := store.ParseLeaseID(string(pl.ID))
		if err != nil {
			return nil, err
		}
		row, err := st.GetLease(ctx, lid)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		kinds := make([]policy.EffectKind, 0, len(pl.Kinds))
		for k := range pl.Kinds {
			kinds = append(kinds, k)
		}
		out = append(out, policy.NewLease(pl.ID, pl.Scope, pl.Principal, row.ExpiresAt, kinds...))
	}
	return out, nil
}

type createLeaseFailer struct {
	store.Store
	mu     sync.Mutex
	failAt int
	n      int
}

func (s *createLeaseFailer) CreateLease(ctx context.Context, l store.Lease) error {
	s.mu.Lock()
	s.n++
	n := s.n
	s.mu.Unlock()
	if n == s.failAt {
		return errors.New("injected create lease failure")
	}
	return s.Store.CreateLease(ctx, l)
}

type hashWorld struct {
	mu      sync.Mutex
	hashes  map[string]string
	applied map[string]string
	fail    error
	cancel  context.CancelFunc
}

func newHashWorld(pre string) *hashWorld {
	return &hashWorld{
		hashes:  map[string]string{"a.txt": pre},
		applied: map[string]string{},
	}
}

func (w *hashWorld) Observe(_ context.Context, target string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hashes[target], nil
}

func (w *hashWorld) Execute(ctx context.Context, row plan.Row) (string, error) {
	if w.cancel != nil {
		w.cancel()
		return "", ctx.Err()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if post, ok := w.applied[row.IdempotencyKey]; ok {
		return post, nil
	}
	if w.fail != nil {
		return "", w.fail
	}
	post := row.Postcondition
	if post == "" {
		post = "obs:" + row.Payload
	}
	w.hashes[row.Target] = post
	w.applied[row.IdempotencyKey] = post
	return post, nil
}

func (w *hashWorld) Seen(key string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	post, ok := w.applied[key]
	return post, ok
}

type staticClock struct{ now time.Time }

func (c staticClock) Now() time.Time { return c.now }
