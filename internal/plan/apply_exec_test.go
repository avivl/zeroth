package plan

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/session"
)

const applyActor policy.PrincipalID = "alice"

type memWorld struct {
	mu      sync.Mutex
	hashes  map[string]string
	applied map[string]string
	writes  []string
	failKey string
	failErr error
	after   func(Row)
}

func newWorld(hashes map[string]string) *memWorld {
	cp := make(map[string]string, len(hashes))
	for k, v := range hashes {
		cp[k] = v
	}
	return &memWorld{hashes: cp, applied: make(map[string]string)}
}

func (w *memWorld) Observe(_ context.Context, target string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hashes[target], nil
}

func (w *memWorld) Execute(_ context.Context, row Row) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if post, ok := w.applied[row.IdempotencyKey]; ok {
		return post, nil
	}
	if w.failKey != "" && row.IdempotencyKey == w.failKey {
		err := w.failErr
		if err == nil {
			err = errors.New("row failed")
		}
		return "", err
	}
	post := row.Postcondition
	if post == "" {
		post = "obs:" + row.Payload
	}
	w.hashes[row.Target] = post
	w.applied[row.IdempotencyKey] = post
	w.writes = append(w.writes, row.IdempotencyKey)
	if w.after != nil {
		w.after(row)
	}
	return post, nil
}

func (w *memWorld) Seen(key string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	post, ok := w.applied[key]
	return post, ok
}

func (w *memWorld) writeKeys() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.writes))
	copy(out, w.writes)
	return out
}

func (w *memWorld) hash(target string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hashes[target]
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

type testLeaser struct {
	mu                 sync.Mutex
	leases             []policy.Lease
	acquires, releases int
}

func (l *testLeaser) Acquire(_ context.Context, _ Plan) ([]policy.Lease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.acquires++
	out := make([]policy.Lease, len(l.leases))
	copy(out, l.leases)
	return out, nil
}

func (l *testLeaser) Release(_ context.Context, _ []policy.Lease) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releases++
	return nil
}

func (l *testLeaser) counts() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acquires, l.releases
}

type testCK struct {
	mu sync.Mutex
	n  int
}

func (c *testCK) Checkpoint(_ context.Context) (CheckpointRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return "ckpt-1", nil
}

func (c *testCK) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

type testAuditor struct{}

func (testAuditor) SignRow(context.Context, Row, string) error { return nil }
func (testAuditor) SignPlan(context.Context, Plan) error       { return nil }

// signPlanFails is a signer that cannot record the plan. SignRow still works,
// so rows apply and the halt path is what trips over the failure.
type signPlanFails struct{ testAuditor }

func (signPlanFails) SignPlan(context.Context, Plan) error {
	return errors.New("signer offline")
}

func threeApproved(t *testing.T) Plan {
	t.Helper()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "modify", Path: "a.txt", Diff: "A"},
			{Type: "modify", Path: "b.txt", Diff: "B"},
			{Type: "modify", Path: "c.txt", Diff: "C"},
		},
		Observed: map[string]string{"a.txt": "h1", "b.txt": "h2", "c.txt": "h3"},
	})
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	approved, err := examined(p).Approve(now)
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func coveringLease(now time.Time) policy.Lease {
	return policy.NewLease("lease-1", "scope-a", applyActor, now.Add(time.Hour),
		OpModify.Kind(), OpCreate.Kind(), OpDestroy.Kind(), OpMemoryProposal.Kind())
}

func applyHarness(now time.Time) (*Applier, *memWorld, *testLeaser, *testCK, *testClock) {
	world := newWorld(map[string]string{"a.txt": "h1", "b.txt": "h2", "c.txt": "h3"})
	clock := &testClock{now: now}
	leases := &testLeaser{leases: []policy.Lease{coveringLease(now)}}
	ck := &testCK{}
	a := &Applier{
		Kernel:      policy.New(),
		Clock:       clock,
		World:       world,
		Leases:      leases,
		Checkpoints: ck,
		Audit:       testAuditor{},
	}
	return a, world, leases, ck, clock
}

func TestApplyHappyPath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, ck, _ := applyHarness(now)
	p := threeApproved(t)

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApplied || got.AppliedThrough != 3 {
		t.Fatalf("status=%q through=%d", got.Status, got.AppliedThrough)
	}
	if got.Plan.AppliedThrough != 3 || got.Plan.Checkpoint == "" {
		t.Fatalf("plan through=%d ck=%q", got.Plan.AppliedThrough, got.Plan.Checkpoint)
	}
	if got.Checkpoint == "" || ck.count() != 1 {
		t.Fatal("expected a checkpoint")
	}
	if len(world.writeKeys()) != 3 {
		t.Fatalf("writes = %v", world.writeKeys())
	}
	acq, rel := leases.counts()
	if acq != 1 || rel != 1 {
		t.Fatalf("leases %d/%d", acq, rel)
	}
}

func TestApplyRefusesOnPreconditionDrift(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, ck, _ := applyHarness(now)
	p := threeApproved(t)
	world.mu.Lock()
	world.hashes["b.txt"] = "mutated"
	world.mu.Unlock()

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v, want ErrStale", err)
	}
	if got.Status != StatusStale || got.AppliedThrough != 0 {
		t.Fatalf("status=%q through=%d", got.Status, got.AppliedThrough)
	}
	if len(world.writeKeys()) != 0 {
		t.Fatalf("writes = %v", world.writeKeys())
	}
	if world.hash("a.txt") != "h1" || world.hash("c.txt") != "h3" {
		t.Fatal("world was written despite refuse")
	}
	acq, rel := leases.counts()
	if acq != 0 || rel != 0 || ck.count() != 0 {
		t.Fatalf("side effects on refuse: leases %d/%d ck %d", acq, rel, ck.count())
	}
}

func TestApplyApprovalDoesNotCoverNewRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, ck, _ := applyHarness(now)
	rev2 := threeApproved(t)
	rev3 := rev2
	rev3.Rows = append([]Row(nil), rev2.Rows...)
	rev3.Rows[1].Payload = "B-revised"
	rev3.Hash = HashOf(rev3)

	got, err := applier.Apply(t.Context(), applyActor, rev3, Approval{PlanHash: rev2.Hash})
	if !errors.Is(err, ErrApproval) {
		t.Fatalf("err = %v, want ErrApproval", err)
	}
	if HashOf(rev3) == rev2.Hash {
		t.Fatal("rev3 hash collided with rev2")
	}
	if got.Status != StatusApproved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	if len(world.writeKeys()) != 0 {
		t.Fatalf("writes = %v", world.writeKeys())
	}
	acq, rel := leases.counts()
	if acq != 0 || rel != 0 || ck.count() != 0 {
		t.Fatalf("side effects on refuse: leases %d/%d ck %d", acq, rel, ck.count())
	}
}

func TestApplyTamperedRowsRefuseWithHashMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, ck, _ := applyHarness(now)
	p := threeApproved(t)
	stored := p.Hash
	p.Rows = append([]Row(nil), p.Rows...)
	p.Rows[0].Payload = "tampered"
	if HashOf(p) == stored {
		t.Fatal("tamper must change HashOf")
	}

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: stored})
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("err = %v, want ErrHashMismatch", err)
	}
	if got.Status != StatusApproved || len(world.writeKeys()) != 0 {
		t.Fatalf("status=%q writes=%v", got.Status, world.writeKeys())
	}
	acq, rel := leases.counts()
	if acq != 0 || rel != 0 || ck.count() != 0 {
		t.Fatalf("side effects on refuse: leases %d/%d ck %d", acq, rel, ck.count())
	}
}

func TestApplyExpiredLeaseRecordsBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, _, clock := applyHarness(now)
	p := threeApproved(t)
	world.after = func(row Row) {
		if row.Target == "a.txt" {
			clock.Set(now.Add(2 * time.Hour))
		}
	}

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	if got.Status != StatusPartiallyApplied || got.AppliedThrough != 1 {
		t.Fatalf("status=%q through=%d", got.Status, got.AppliedThrough)
	}
	if got.Plan.AppliedThrough != 1 {
		t.Fatalf("plan through=%d", got.Plan.AppliedThrough)
	}
	writes := world.writeKeys()
	if len(writes) != 1 || writes[0] != p.Rows[0].IdempotencyKey {
		t.Fatalf("writes = %v", writes)
	}
	if world.hash("a.txt") != p.Rows[0].Postcondition || world.hash("b.txt") != "h2" || world.hash("c.txt") != "h3" {
		t.Fatalf("incoherent world a=%q b=%q c=%q", world.hash("a.txt"), world.hash("b.txt"), world.hash("c.txt"))
	}
	acq, rel := leases.counts()
	if acq != 1 || rel != 1 {
		t.Fatalf("leases %d/%d", acq, rel)
	}
}

func TestApplyEffectFailureRecordsCoherentState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, _, _ := applyHarness(now)
	p := threeApproved(t)
	world.failKey = p.Rows[1].IdempotencyKey
	world.failErr = errors.New("disk full")

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	if got.Status != StatusPartiallyApplied || got.AppliedThrough != 1 {
		t.Fatalf("status=%q through=%d", got.Status, got.AppliedThrough)
	}
	if got.Plan.AppliedThrough != 1 {
		t.Fatalf("plan through=%d", got.Plan.AppliedThrough)
	}
	if len(got.Applied) != 1 || got.Applied[0].Postcondition != p.Rows[0].Postcondition {
		t.Fatalf("applied = %+v", got.Applied)
	}
	if world.hash("a.txt") != p.Rows[0].Postcondition || world.hash("b.txt") != "h2" || world.hash("c.txt") != "h3" {
		t.Fatalf("incoherent world a=%q b=%q c=%q", world.hash("a.txt"), world.hash("b.txt"), world.hash("c.txt"))
	}
	acq, rel := leases.counts()
	if acq != 1 || rel != 1 {
		t.Fatalf("leases %d/%d", acq, rel)
	}
}

func TestApplyReplayByIdempotencyKeyIsNoOp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, _, _, _ := applyHarness(now)
	p := threeApproved(t)
	approval := Approval{PlanHash: p.Hash}

	first, err := applier.Apply(t.Context(), applyActor, p, approval)
	if err != nil {
		t.Fatal(err)
	}
	if len(world.writeKeys()) != 3 {
		t.Fatalf("first writes = %v", world.writeKeys())
	}

	again, err := applier.Apply(t.Context(), applyActor, first.Plan, approval)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != StatusApplied || len(world.writeKeys()) != 3 {
		t.Fatalf("StatusApplied retry wrote: status=%q writes=%v", again.Status, world.writeKeys())
	}

	retry := first.Plan
	retry.Status = StatusApproved
	second, err := applier.Apply(t.Context(), applyActor, retry, approval)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != StatusApplied || second.AppliedThrough != 3 {
		t.Fatalf("key replay status=%q through=%d", second.Status, second.AppliedThrough)
	}
	if len(world.writeKeys()) != 3 {
		t.Fatalf("idempotent replay wrote again: %v", world.writeKeys())
	}
}

func TestApplyIdenticalEffectsOnDistinctSessionsBothExecute(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	world := newWorld(nil)
	clock := &testClock{now: now}
	leases := &testLeaser{leases: []policy.Lease{coveringLease(now)}}
	applier := &Applier{
		Kernel:      policy.New(),
		Clock:       clock,
		World:       world,
		Leases:      leases,
		Checkpoints: &testCK{},
		Audit:       testAuditor{},
	}
	effect := []Proposed{{Type: "create", Path: "CHANGELOG.md", Diff: "# Changelog\n"}}
	first := approveCreate(t, "sess-1", "plan-a", effect, now)
	second := approveCreate(t, "sess-2", "plan-b", effect, now)
	if first.Rows[0].IdempotencyKey == second.Rows[0].IdempotencyKey {
		t.Fatal("distinct sessions shared an idempotency key")
	}

	got1, err := applier.Apply(t.Context(), applyActor, first, Approval{PlanHash: first.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if got1.Status != StatusApplied {
		t.Fatalf("first status %s", got1.Status)
	}
	// Distinct sessions have distinct overlays. Clear the file hash so
	// Observe looks like a fresh workspace, while keeping the Seen map
	// that caused cross-run collisions.
	world.mu.Lock()
	delete(world.hashes, "CHANGELOG.md")
	world.mu.Unlock()
	got2, err := applier.Apply(t.Context(), applyActor, second, Approval{PlanHash: second.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if got2.Status != StatusApplied {
		t.Fatalf("second status %s", got2.Status)
	}
	writes := world.writeKeys()
	if len(writes) != 2 {
		t.Fatalf("shared world wrote %v, want both sessions to Execute", writes)
	}
	if writes[0] == writes[1] {
		t.Fatal("both Executes used the same idempotency key")
	}
}

func approveCreate(t *testing.T, sessRaw, planRaw string, effects []Proposed, now time.Time) Plan {
	t.Helper()
	d := validDraft()
	id, err := ParseID(planRaw)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.ParseID(sessRaw)
	if err != nil {
		t.Fatal(err)
	}
	d.ID = id
	d.SessionID = sess
	d.Effects = effects
	d.Observed = nil
	p, err := Build(d)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := examined(p).Approve(now)
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func TestApplyRefusesBlindPartialResume(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, _, _, _ := applyHarness(now)
	p := threeApproved(t)
	world.failKey = p.Rows[1].IdempotencyKey

	partial, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("err = %v", err)
	}
	if partial.Plan.AppliedThrough != 1 {
		t.Fatalf("boundary lost on plan: %d", partial.Plan.AppliedThrough)
	}
	world.failKey = ""
	resume, err := applier.Apply(t.Context(), applyActor, partial.Plan, Approval{PlanHash: p.Hash})
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("resume err = %v, want ErrPartial", err)
	}
	if resume.AppliedThrough != 1 {
		t.Fatalf("resume through = %d, want recorded boundary", resume.AppliedThrough)
	}
	if len(world.writeKeys()) != 1 {
		t.Fatalf("blind resume wrote: %v", world.writeKeys())
	}
}

func TestApplySecretScanBlocks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, ck, _ := applyHarness(now)
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "modify", Path: "a.txt", Diff: "A"},
			{Type: "modify", Path: "b.txt", Diff: "token " + "ghp_" + strings.Repeat("A", 36)},
			{Type: "modify", Path: "c.txt", Diff: "C"},
		},
		Observed: map[string]string{"a.txt": "h1", "b.txt": "h2", "c.txt": "h3"},
	})
	approved, err := examined(p).Approve(now)
	if err != nil {
		t.Fatal(err)
	}

	got, err := applier.Apply(t.Context(), applyActor, approved, Approval{PlanHash: approved.Hash})
	if !errors.Is(err, ErrSecret) {
		t.Fatalf("err = %v, want ErrSecret", err)
	}
	if got.Status != StatusApproved || len(world.writeKeys()) != 0 {
		t.Fatalf("status=%q writes=%v", got.Status, world.writeKeys())
	}
	acq, rel := leases.counts()
	if acq != 1 || rel != 1 || ck.count() != 1 {
		t.Fatalf("leases %d/%d ck %d", acq, rel, ck.count())
	}
}

func TestApplyRefusesOnPostconditionMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	inner, leases, ck := func() (*memWorld, *testLeaser, *testCK) {
		applier, world, leases, ck, _ := applyHarness(now)
		_ = applier
		return world, leases, ck
	}()
	applier := &Applier{
		Kernel:      policy.New(),
		Clock:       &testClock{now: now},
		World:       lyingWorld{memWorld: inner},
		Leases:      leases,
		Checkpoints: ck,
		Audit:       testAuditor{},
	}
	p := threeApproved(t)

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if !errors.Is(err, ErrPostcondition) {
		t.Fatalf("err = %v, want ErrPostcondition", err)
	}
	if got.AppliedThrough != 0 {
		t.Fatalf("through=%d, want 0 so a mismatch cannot land a PR", got.AppliedThrough)
	}
	acq, rel := leases.counts()
	if acq != 1 || rel != 1 || ck.count() != 1 {
		t.Fatalf("leases %d/%d ck %d", acq, rel, ck.count())
	}
}

type lyingWorld struct {
	*memWorld
}

func (w lyingWorld) Execute(ctx context.Context, row Row) (string, error) {
	if _, err := w.memWorld.Execute(ctx, row); err != nil {
		return "", err
	}
	return "not-the-recorded-post", nil
}

func TestApplyMissingPorts(t *testing.T) {
	t.Parallel()
	_, err := (*Applier)(nil).Apply(t.Context(), applyActor, Plan{}, Approval{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

type testMemory struct {
	mu        sync.Mutex
	proposals []Row
	fail      error
}

func (m *testMemory) Propose(_ context.Context, row Row) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return "", m.fail
	}
	m.proposals = append(m.proposals, row)
	if row.Postcondition != "" {
		return row.Postcondition, nil
	}
	return "queued:" + row.Target, nil
}

func (m *testMemory) queued() []Row {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Row, len(m.proposals))
	copy(out, m.proposals)
	return out
}

func memoryApproved(t *testing.T) Plan {
	t.Helper()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "modify", Path: "a.txt", Diff: "A"},
			{Type: "memory_proposal", Path: "session/style", Diff: "prefer table tests"},
		},
		Observed: map[string]string{"a.txt": "h1"},
	})
	approved, err := examined(p).Approve(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func TestApplyMemoryProposalQueuesAndDoesNotWriteWorld(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, _, _, _ := applyHarness(now)
	mem := &testMemory{}
	applier.Memory = mem
	p := memoryApproved(t)

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApplied || got.AppliedThrough != 2 {
		t.Fatalf("status=%q through=%d", got.Status, got.AppliedThrough)
	}
	if len(world.writeKeys()) != 1 || world.writeKeys()[0] != p.Rows[0].IdempotencyKey {
		t.Fatalf("world writes = %v", world.writeKeys())
	}
	queued := mem.queued()
	if len(queued) != 1 || queued[0].Op != OpMemoryProposal || queued[0].Target != "session/style" {
		t.Fatalf("queued %+v", queued)
	}
	if queued[0].Payload != "prefer table tests" {
		t.Fatalf("payload %q", queued[0].Payload)
	}
}

func TestApplyMemoryProposalWithoutQueueIsFailClosed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, ck, _ := applyHarness(now)
	p := memoryApproved(t)

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if got.AppliedThrough != 0 || len(world.writeKeys()) != 0 {
		t.Fatalf("status=%q through=%d writes=%v", got.Status, got.AppliedThrough, world.writeKeys())
	}
	acq, rel := leases.counts()
	if acq != 0 || rel != 0 || ck.count() != 0 {
		t.Fatalf("side effects on refuse: leases %d/%d ck %d", acq, rel, ck.count())
	}
}

func TestApplyFirstRowFailureIsFailClosed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, _, _ := applyHarness(now)
	p := threeApproved(t)
	world.failKey = p.Rows[0].IdempotencyKey

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if errors.Is(err, ErrPartial) || err == nil {
		t.Fatalf("err = %v, nothing landed so not partial", err)
	}
	if got.Status != StatusApproved || got.AppliedThrough != 0 || len(world.writeKeys()) != 0 {
		t.Fatalf("status=%q through=%d writes=%v", got.Status, got.AppliedThrough, world.writeKeys())
	}
	acq, rel := leases.counts()
	if acq != 1 || rel != 1 {
		t.Fatalf("leases %d/%d", acq, rel)
	}
}

func TestApplyReleasesOnCancel(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, leases, _, _ := applyHarness(now)
	p := threeApproved(t)
	ctx, cancel := context.WithCancel(t.Context())
	world.after = func(Row) { cancel() }

	got, err := applier.Apply(ctx, applyActor, p, Approval{PlanHash: p.Hash})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got.AppliedThrough < 1 {
		t.Fatalf("through=%d, first row should have landed before cancel", got.AppliedThrough)
	}
	acq, rel := leases.counts()
	if acq != 1 || rel != 1 {
		t.Fatalf("leases %d/%d", acq, rel)
	}
}

func TestPropertyPreconditionDriftWritesNothing(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(11))
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 200; i++ {
		n := r.Intn(3) + 1
		effects := make([]Proposed, n)
		observed := make(map[string]string, n)
		live := make(map[string]string, n)
		for j := 0; j < n; j++ {
			path := fmt.Sprintf("f-%d.txt", j)
			effects[j] = Proposed{Type: "modify", Path: path, Diff: fmt.Sprintf("p-%d", j)}
			observed[path] = fmt.Sprintf("h-%d", j)
			live[path] = observed[path]
		}
		drift := r.Intn(n)
		live[effects[drift].Path] = fmt.Sprintf("mutated-%d", i)

		p := mustBuild(t, Draft{Effects: effects, Observed: observed})
		approved, err := examined(p).Approve(now)
		if err != nil {
			t.Fatal(err)
		}
		world := newWorld(live)
		applier := &Applier{
			Kernel:      policy.New(),
			Clock:       &testClock{now: now},
			World:       world,
			Leases:      &testLeaser{leases: []policy.Lease{coveringLease(now)}},
			Checkpoints: &testCK{},
			Audit:       testAuditor{},
		}
		got, err := applier.Apply(t.Context(), applyActor, approved, Approval{PlanHash: approved.Hash})
		if !errors.Is(err, ErrStale) || got.Status != StatusStale || len(world.writeKeys()) != 0 {
			t.Fatalf("iteration %d: err=%v status=%q writes=%v", i, err, got.Status, world.writeKeys())
		}
	}
}

func TestPropertyApprovalBindsToExactHash(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(12))
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 200; i++ {
		n := r.Intn(3) + 1
		effects := make([]Proposed, n)
		observed := make(map[string]string, n)
		for j := 0; j < n; j++ {
			path := fmt.Sprintf("f-%d.txt", j)
			effects[j] = Proposed{Type: "modify", Path: path, Diff: fmt.Sprintf("p-%d-%d", i, j)}
			observed[path] = fmt.Sprintf("h-%d", j)
		}
		rev2 := mustBuild(t, Draft{Effects: effects, Observed: observed})
		approved2, err := examined(rev2).Approve(now)
		if err != nil {
			t.Fatal(err)
		}
		rev3effects := append([]Proposed(nil), effects...)
		rev3effects[0].Diff = rev3effects[0].Diff + "-rev3"
		rev3 := mustBuild(t, Draft{Effects: rev3effects, Observed: observed})
		rev3.Status = StatusApproved

		world := newWorld(observed)
		applier := &Applier{
			Kernel:      policy.New(),
			Clock:       &testClock{now: now},
			World:       world,
			Leases:      &testLeaser{leases: []policy.Lease{coveringLease(now)}},
			Checkpoints: &testCK{},
			Audit:       testAuditor{},
		}
		_, err = applier.Apply(t.Context(), applyActor, rev3, Approval{PlanHash: approved2.Hash})
		if !errors.Is(err, ErrApproval) || len(world.writeKeys()) != 0 {
			t.Fatalf("iteration %d: err=%v writes=%v", i, err, world.writeKeys())
		}
	}
}

func TestPropertyExpiredLeaseNeverAppliesPastWindow(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(13))
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 200; i++ {
		n := r.Intn(3) + 2
		landBeforeExpire := r.Intn(n-1) + 1
		effects := make([]Proposed, n)
		observed := make(map[string]string, n)
		for j := 0; j < n; j++ {
			path := fmt.Sprintf("f-%d.txt", j)
			effects[j] = Proposed{Type: "modify", Path: path, Diff: fmt.Sprintf("p-%d", j)}
			observed[path] = fmt.Sprintf("h-%d", j)
		}
		p := mustBuild(t, Draft{Effects: effects, Observed: observed})
		approved, err := examined(p).Approve(now)
		if err != nil {
			t.Fatal(err)
		}
		world := newWorld(observed)
		clock := &testClock{now: now}
		var landed int
		world.after = func(Row) {
			landed++
			if landed == landBeforeExpire {
				clock.Set(now.Add(2 * time.Hour))
			}
		}
		applier := &Applier{
			Kernel:      policy.New(),
			Clock:       clock,
			World:       world,
			Leases:      &testLeaser{leases: []policy.Lease{coveringLease(now)}},
			Checkpoints: &testCK{},
			Audit:       testAuditor{},
		}
		got, err := applier.Apply(t.Context(), applyActor, approved, Approval{PlanHash: approved.Hash})
		if !errors.Is(err, ErrPartial) {
			t.Fatalf("iteration %d: err=%v", i, err)
		}
		if got.AppliedThrough != landBeforeExpire || len(world.writeKeys()) != landBeforeExpire {
			t.Fatalf("iteration %d: through=%d writes=%v want %d", i, got.AppliedThrough, world.writeKeys(), landBeforeExpire)
		}
	}
}

func TestHaltPartialSurfacesSignFailure(t *testing.T) {
	t.Parallel()
	// 42-65: a partial apply that the signed chain never recorded is worse
	// than the failure that caused the halt. Discarding the SignPlan error
	// left the caller seeing only the original cause.
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	applier, world, _, _, clock := applyHarness(now)
	applier.Audit = signPlanFails{}
	p := threeApproved(t)
	world.after = func(row Row) {
		if row.Target == "a.txt" {
			clock.Set(now.Add(2 * time.Hour))
		}
	}

	got, err := applier.Apply(t.Context(), applyActor, p, Approval{PlanHash: p.Hash})
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	if !strings.Contains(err.Error(), "signer offline") {
		t.Fatalf("err = %v, want it to report the unrecorded halt", err)
	}
	if got.Status != StatusPartiallyApplied || got.AppliedThrough != 1 {
		t.Fatalf("status=%q through=%d", got.Status, got.AppliedThrough)
	}
}
