package server_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// failAuditStore breaks the signed chain for one action only, so a run and its
// agent key can still be set up before the action under test is attempted.
type failAuditStore struct {
	store.Store
	action string
}

func (f *failAuditStore) AppendAudit(ctx context.Context, r store.AuditRecord) (store.AuditRecord, error) {
	if r.Action == f.action {
		return store.AuditRecord{}, errors.New("audit store unavailable")
	}
	return f.Store.AppendAudit(ctx, r)
}

func failAuditOn(action string) func(store.Store) store.Store {
	return func(st store.Store) store.Store {
		return &failAuditStore{Store: st, action: action}
	}
}

// seedForAudit seeds a draft, cross-examining it when the action under test
// needs a plan that is already pending approval.
func seedForAudit(t *testing.T, e *examEnv, examine bool) store.PlanID {
	t.Helper()
	e.patchReviewer(false)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	pid := e.seedPlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	}, map[string]string{"docs/design/plan.md": "pre"})
	if examine {
		if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
			t.Fatal(err)
		}
	}
	return pid
}

func TestPlanActionsFailClosedOnAuditAppend(t *testing.T) {
	t.Parallel()
	// 42-65: these three discarded audit.Append's error and reported success,
	// so an action could complete while the signed chain never recorded it.
	cases := []struct {
		name string
		// action is the chain entry to break, and the string the handler has
		// to name so a failure here is not confused with any other 500.
		action string
		path   string
		body   any
		// examine seeds a cross-examined plan. Approve and request-changes
		// need one; branch must not have one, or the branch's own exam trips
		// an illegal transition and 500s for the wrong reason.
		examine bool
	}{
		{"approve", audit.ActionPlanApprove, "/approve", gen.ApproveRequest{}, true},
		{"request-changes", audit.ActionPlanReject, "/request-changes", gen.RequestChangesRequest{Comment: "narrow the diff"}, true},
		{"branch", audit.ActionPlanBranch, "/branch", gen.BranchPlanRequest{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := examSetupWith(t, &capturingReviewer{}, failAuditOn(tc.action))
			pid := seedForAudit(t, e, tc.examine)

			res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+tc.path, tc.body)
			defer res.Body.Close()
			slurp, _ := io.ReadAll(res.Body)
			if res.StatusCode < 500 {
				t.Fatalf("status = %d, want a server error when the audit append fails: %s", res.StatusCode, slurp)
			}
			if !strings.Contains(string(slurp), tc.action) {
				t.Fatalf("error body = %s, want it to name the %s audit failure", slurp, tc.action)
			}
		})
	}
}

func TestCheckpointFailsClosedOnAuditAppend(t *testing.T) {
	t.Parallel()
	// 42-65: the checkpoint row and its tar were written, then the audit
	// append error was dropped and the API reported a clean creation.
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         failAuditOn(audit.ActionCheckpoint)(st),
		Sandbox:       newFakeSandbox(),
		CheckpointDir: t.TempDir(),
		TokenInterval: time.Hour,
		TokenCount:    1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	run := createRun(t, hs.URL, "checkpoint audit fail-closed")
	res := postJSON(t, hs.URL+"/runs/"+string(run.Id)+"/checkpoints", gen.CreateCheckpointRequest{})
	defer res.Body.Close()
	slurp, _ := io.ReadAll(res.Body)
	if res.StatusCode < 500 {
		t.Fatalf("status = %d, want a server error when the audit append fails: %s", res.StatusCode, slurp)
	}
	if !strings.Contains(string(slurp), audit.ActionCheckpoint) {
		t.Fatalf("error body = %s, want it to name the %s audit failure", slurp, audit.ActionCheckpoint)
	}
}
