package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// hookStore wraps a Store so tests can inject list/update/sync failures
// after setup has already succeeded.
type hookStore struct {
	store.Store
	mu             sync.Mutex
	updateSession  func(ctx context.Context, s store.Session) error
	appendEvent    func(ctx context.Context, sessionID store.SessionID, ev store.Event) (store.Event, error)
	listApprovals  func(ctx context.Context, q store.ApprovalQuery) (store.Page[store.Approval], error)
	updateApproval func(ctx context.Context, a store.Approval) error
}

func (h *hookStore) setUpdateSession(fn func(ctx context.Context, s store.Session) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updateSession = fn
}

func (h *hookStore) setListApprovals(fn func(ctx context.Context, q store.ApprovalQuery) (store.Page[store.Approval], error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listApprovals = fn
}

func (h *hookStore) setUpdateApproval(fn func(ctx context.Context, a store.Approval) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updateApproval = fn
}

func (h *hookStore) setAppendEvent(fn func(ctx context.Context, sessionID store.SessionID, ev store.Event) (store.Event, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.appendEvent = fn
}

func (h *hookStore) UpdateSession(ctx context.Context, s store.Session) error {
	h.mu.Lock()
	fn := h.updateSession
	h.mu.Unlock()
	if fn != nil {
		return fn(ctx, s)
	}
	return h.Store.UpdateSession(ctx, s)
}

func (h *hookStore) AppendEvent(ctx context.Context, sessionID store.SessionID, ev store.Event) (store.Event, error) {
	h.mu.Lock()
	fn := h.appendEvent
	h.mu.Unlock()
	if fn != nil {
		return fn(ctx, sessionID, ev)
	}
	return h.Store.AppendEvent(ctx, sessionID, ev)
}

func (h *hookStore) ListApprovals(ctx context.Context, q store.ApprovalQuery) (store.Page[store.Approval], error) {
	h.mu.Lock()
	fn := h.listApprovals
	h.mu.Unlock()
	if fn != nil {
		return fn(ctx, q)
	}
	return h.Store.ListApprovals(ctx, q)
}

func (h *hookStore) UpdateApproval(ctx context.Context, a store.Approval) error {
	h.mu.Lock()
	fn := h.updateApproval
	h.mu.Unlock()
	if fn != nil {
		return fn(ctx, a)
	}
	return h.Store.UpdateApproval(ctx, a)
}

func newHookStore(t *testing.T) *hookStore {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &hookStore{Store: st}
}

func TestApplyPlanBookkeepingFailureIsNot200(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		arm  func(*hookStore, error)
	}{
		{
			name: "syncSession",
			arm: func(h *hookStore, err error) {
				h.setUpdateSession(func(ctx context.Context, sess store.Session) error {
					if sess.Status == string(gen.RunStatusCompleted) {
						return err
					}
					return h.Store.UpdateSession(ctx, sess)
				})
			},
		},
		{
			name: "succeed",
			arm: func(h *hookStore, err error) {
				h.setAppendEvent(func(ctx context.Context, sessionID store.SessionID, ev store.Event) (store.Event, error) {
					if ev.Type == string(session.EventTerminal) && ev.Payload == session.PayloadDone {
						return store.Event{}, err
					}
					return h.Store.AppendEvent(ctx, sessionID, ev)
				})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hooks := newHookStore(t)
			e := applySetupOn(t, hooks, nil)
			e.patchReviewer(false)
			run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
			pid := e.seedPatchedFilePlan(run, []plan.Proposed{
				{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
			})
			if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
				t.Fatal(err)
			}
			comment := "ship it"
			res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("approve %d", res.StatusCode)
			}

			injected := errors.New("injected " + tc.name + " failure")
			tc.arm(hooks, injected)

			applied := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/apply", struct{}{})
			defer applied.Body.Close()
			slurp, _ := io.ReadAll(applied.Body)
			if applied.StatusCode == http.StatusOK {
				t.Fatalf("apply returned 200 after %s failure: %s", tc.name, slurp)
			}
			if applied.StatusCode != http.StatusInternalServerError {
				t.Fatalf("apply %d %s, want 500", applied.StatusCode, slurp)
			}
			if !strings.Contains(string(slurp), injected.Error()) {
				t.Fatalf("apply hid the %s failure: %s", tc.name, slurp)
			}
			var out gen.Error
			if err := json.Unmarshal(slurp, &out); err != nil {
				t.Fatal(err)
			}
			if out.Code != "internal" {
				t.Fatalf("code %q", out.Code)
			}
		})
	}
}

func TestMarkApprovalsStoreFailureIsNotSilentSuccess(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		logMsg  string
		arm     func(*hookStore, error)
		wantSub string
	}{
		{
			name:   "list",
			logMsg: "mark approvals list",
			arm: func(h *hookStore, err error) {
				h.setListApprovals(func(context.Context, store.ApprovalQuery) (store.Page[store.Approval], error) {
					return store.Page[store.Approval]{}, err
				})
			},
			wantSub: "mark approvals list",
		},
		{
			name:   "update",
			logMsg: "mark approvals update",
			arm: func(h *hookStore, err error) {
				h.setUpdateApproval(func(context.Context, store.Approval) error {
					return err
				})
			},
			wantSub: "mark approvals update",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hooks := newHookStore(t)
			core, logs := observer.New(zapcore.ErrorLevel)
			e := applySetupOn(t, hooks, zap.New(core))
			e.patchReviewer(false)
			run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
			pid := e.seedPatchedFilePlan(run, []plan.Proposed{
				{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
			})
			if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
				t.Fatal(err)
			}

			injected := errors.New("injected markApprovals " + tc.name + " failure")
			tc.arm(hooks, injected)

			comment := "ship it"
			res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/approve", gen.ApproveRequest{Comment: &comment})
			defer res.Body.Close()
			slurp, _ := io.ReadAll(res.Body)
			if res.StatusCode == http.StatusOK {
				t.Fatalf("approve returned 200 after markApprovals %s failure: %s", tc.name, slurp)
			}
			if res.StatusCode != http.StatusInternalServerError {
				t.Fatalf("approve %d %s, want 500", res.StatusCode, slurp)
			}
			if !strings.Contains(string(slurp), injected.Error()) {
				t.Fatalf("approve hid the markApprovals %s failure: %s", tc.name, slurp)
			}
			if !strings.Contains(string(slurp), tc.wantSub) {
				t.Fatalf("approve missing %q: %s", tc.wantSub, slurp)
			}

			got := logs.FilterMessage(tc.logMsg)
			if got.Len() == 0 {
				t.Fatalf("expected error log %q, got %+v", tc.logMsg, logs.All())
			}

			hooks.setListApprovals(nil)
			hooks.setUpdateApproval(nil)
			inbox, err := http.Get(e.hs.URL + "/approvals?status=pending")
			if err != nil {
				t.Fatal(err)
			}
			defer inbox.Body.Close()
			if inbox.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(inbox.Body)
				t.Fatalf("inbox %d %s", inbox.StatusCode, body)
			}
			var approvals gen.ApprovalList
			if err := json.NewDecoder(inbox.Body).Decode(&approvals); err != nil {
				t.Fatal(err)
			}
			if len(approvals.Items) != 1 || approvals.Items[0].Status != gen.ApprovalStatusPending {
				t.Fatalf("inbox after failed markApprovals %+v", approvals.Items)
			}
		})
	}
}

func TestRequestPlanChangesMarkApprovalsFailureIsNot200(t *testing.T) {
	t.Parallel()
	hooks := newHookStore(t)
	core, logs := observer.New(zapcore.ErrorLevel)
	e := applySetupOn(t, hooks, zap.New(core))
	e.patchReviewer(false)
	run := createRun(t, e.hs.URL, "Allowed-paths: docs/")
	pid := e.seedPatchedFilePlan(run, []plan.Proposed{
		{Type: "modify", Path: "docs/design/plan.md", Diff: "-typo\n+fixed"},
	})
	if _, err := e.srv.ExamineDraft(t.Context(), pid); err != nil {
		t.Fatal(err)
	}

	injected := errors.New("injected markApprovals list failure")
	hooks.setListApprovals(func(context.Context, store.ApprovalQuery) (store.Page[store.Approval], error) {
		return store.Page[store.Approval]{}, injected
	})

	res := postJSON(t, e.hs.URL+"/plans/"+pid.String()+"/request-changes", gen.RequestChangesRequest{Comment: "narrow the diff"})
	defer res.Body.Close()
	slurp, _ := io.ReadAll(res.Body)
	if res.StatusCode == http.StatusOK {
		t.Fatalf("request-changes returned 200 after markApprovals failure: %s", slurp)
	}
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("request-changes %d %s, want 500", res.StatusCode, slurp)
	}
	if !strings.Contains(string(slurp), injected.Error()) {
		t.Fatalf("request-changes hid the markApprovals failure: %s", slurp)
	}
	if logs.FilterMessage("mark approvals list").Len() == 0 {
		t.Fatalf("expected mark approvals list log, got %+v", logs.All())
	}
}
