package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/harness/claudecode"
	"github.com/avivl/zeroth/internal/session"
)

const conformanceKey = "sk-ant-zeroth-conformance-not-a-real-key"

type adapter struct {
	name string
	open func(t *testing.T) harness.Driver
}

func TestDriverConformance(t *testing.T) {
	t.Parallel()
	bin := buildFakeCLI(t)

	// Adding an adapter is one more row. The cases below must stay
	// implementation-agnostic (NFR-4, Z1-006).
	cases := []adapter{
		{
			name: "claudecode",
			open: func(t *testing.T) harness.Driver {
				t.Helper()
				return claudecode.NewWithBin(bin)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := tc.open(t)
			if d == nil {
				t.Fatal("nil driver")
			}
			if got := d.Name(); got != tc.name {
				t.Fatalf("Name() = %q, want %q", got, tc.name)
			}

			t.Run("start_stream_stop", func(t *testing.T) { testStartStreamStop(t, tc.open) })
			t.Run("steer_mid_run", func(t *testing.T) { testSteerMidRun(t, tc.open) })
			t.Run("stop_detects_kill", func(t *testing.T) { testStopDetectsKill(t, tc.open) })
			t.Run("effects_for_plan_builder", func(t *testing.T) { testEffectsForPlanBuilder(t, tc.open) })
			t.Run("stream_into_session_log", func(t *testing.T) { testStreamIntoSessionLog(t, tc.open) })
			t.Run("no_credential_in_workspace", func(t *testing.T) { testNoCredentialInWorkspace(t, tc.open) })
			t.Run("checkpoint", func(t *testing.T) { testCheckpoint(t, tc.open) })
			t.Run("unknown_id", func(t *testing.T) { testUnknownID(t, tc.open) })
			t.Run("stop_idempotent", func(t *testing.T) { testStopIdempotent(t, tc.open) })
			t.Run("invalid_spec", func(t *testing.T) { testInvalidSpec(t, tc.open) })
		})
	}
}

func testStartStreamStop(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	h := mustStart(t, d, "stream please")
	ch := mustStream(t, d, h.ID)
	got := waitFor(t, ch, 10*time.Second, func(evs []harness.Event) bool {
		return hasKind(evs, harness.EventToken) && hasKind(evs, harness.EventToolCall) && hasKind(evs, harness.EventEffects)
	})
	if payloadContains(got, conformanceKey) {
		t.Fatal("api key leaked into stream events")
	}
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitClosed(t, ch, 5*time.Second)
}

func testSteerMidRun(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	h := mustStart(t, d, "HOLD please")
	ch := mustStream(t, d, h.ID)
	waitFor(t, ch, 10*time.Second, func(evs []harness.Event) bool {
		return tokenHas(evs, "hello-token")
	})
	if err := d.Steer(t.Context(), h.ID, "change course"); err != nil {
		t.Fatalf("Steer: %v", err)
	}
	got := waitFor(t, ch, 10*time.Second, func(evs []harness.Event) bool {
		return tokenHas(evs, "steered-ok")
	})
	if !tokenHas(got, "steered-ok") {
		t.Fatalf("steer did not change behavior: %#v", got)
	}
}

func testStopDetectsKill(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	h := mustStart(t, d, "SLEEP please")
	ch := mustStream(t, d, h.ID)
	waitFor(t, ch, 10*time.Second, func(evs []harness.Event) bool {
		return hasKind(evs, harness.EventToken)
	})
	start := time.Now()
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got := waitFor(t, ch, 8*time.Second, func(evs []harness.Event) bool {
		return hasKind(evs, harness.EventExited)
	})
	if time.Since(start) > 6*time.Second {
		t.Fatalf("Stop hung: %s", time.Since(start))
	}
	if !hasKind(got, harness.EventExited) {
		t.Fatalf("missing exited event: %#v", got)
	}
	waitClosed(t, ch, 3*time.Second)
}

func testEffectsForPlanBuilder(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	h := mustStart(t, d, "propose a change")
	ch := mustStream(t, d, h.ID)
	got := waitFor(t, ch, 10*time.Second, func(evs []harness.Event) bool {
		return hasKind(evs, harness.EventEffects)
	})
	var effects []harness.Effect
	for _, ev := range got {
		if ev.Kind == harness.EventEffects {
			effects = ev.Effects
		}
	}
	if len(effects) == 0 {
		t.Fatal("no effects")
	}
	for i, e := range effects {
		if e.Type == "" || e.Path == "" || e.Diff == "" {
			t.Fatalf("effect %d missing type/path/diff: %+v", i, e)
		}
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var round struct {
			Type string `json:"type"`
			Path string `json:"path"`
			Diff string `json:"diff"`
		}
		if err := json.Unmarshal(raw, &round); err != nil {
			t.Fatal(err)
		}
		if round.Type != e.Type || round.Path != e.Path {
			t.Fatalf("plan-builder JSON: %s", raw)
		}
	}
}

func testStreamIntoSessionLog(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	log := session.NewMemoryLog()
	sup, err := session.NewSupervisor(log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sup.Close)
	sid, err := sup.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	h := mustStart(t, d, "into the log")
	ch := mustStream(t, d, h.ID)
	got := waitFor(t, ch, 10*time.Second, func(evs []harness.Event) bool {
		return hasKind(evs, harness.EventToken) && hasKind(evs, harness.EventToolCall)
	})
	for _, ev := range got {
		switch ev.Kind {
		case harness.EventToken:
			if err := sup.EmitToken(t.Context(), sid, ev.Payload); err != nil {
				t.Fatalf("EmitToken: %v", err)
			}
		case harness.EventToolCall:
			if err := sup.EmitToolCall(t.Context(), sid, ev.Payload); err != nil {
				t.Fatalf("EmitToolCall: %v", err)
			}
		case harness.EventEffects:
			body, err := json.Marshal(ev.Effects)
			if err != nil {
				t.Fatal(err)
			}
			if err := sup.ProposePlan(t.Context(), sid, string(body)); err != nil {
				t.Fatalf("ProposePlan: %v", err)
			}
		}
	}
	evs, err := sup.Events(t.Context(), sid)
	if err != nil {
		t.Fatal(err)
	}
	var tok, tool bool
	for _, ev := range evs {
		switch ev.Type {
		case session.EventToken:
			tok = true
		case session.EventToolCall:
			tool = true
		}
	}
	if !tok || !tool {
		t.Fatalf("session log missing token/tool_call: %+v", evs)
	}
}

func testNoCredentialInWorkspace(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "canary.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := d.Start(t.Context(), specIn(t, ws, "do not leak"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })
	ch := mustStream(t, d, h.ID)
	got := waitFor(t, ch, 10*time.Second, func(evs []harness.Event) bool {
		return hasKind(evs, harness.EventEffects) || hasKind(evs, harness.EventExited)
	})
	if payloadContains(got, conformanceKey) {
		t.Fatal("credential leaked into events")
	}
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	err = filepath.WalkDir(ws, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), conformanceKey) {
			return fmtWalk(path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	keep, err := os.ReadFile(filepath.Join(ws, "canary.txt"))
	if err != nil || string(keep) != "keep" {
		t.Fatalf("workspace canary: %s err=%v", keep, err)
	}
}

type walkErr string

func fmtWalk(path string) error { return walkErr(path) }

func (e walkErr) Error() string {
	return "credential landed in workspace file " + string(e)
}

func testCheckpoint(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	h := mustStart(t, d, "HOLD checkpoint")
	ch := mustStream(t, d, h.ID)
	waitFor(t, ch, 10*time.Second, func(evs []harness.Event) bool {
		return hasKind(evs, harness.EventToken)
	})
	ck, err := d.Checkpoint(t.Context(), h.ID)
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if ck.VendorSession == "" && ck.Transcript == "" {
		t.Fatal("empty checkpoint")
	}
	if strings.Contains(ck.Transcript, conformanceKey) {
		t.Fatal("checkpoint transcript contains api key")
	}
}

func testUnknownID(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	id, err := harness.ParseID("hrn_missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Stream(t.Context(), id); !errors.Is(err, harness.ErrNotFound) {
		t.Fatalf("stream missing: %v, want ErrNotFound", err)
	}
	if _, err := d.Checkpoint(t.Context(), id); !errors.Is(err, harness.ErrNotFound) {
		t.Fatalf("checkpoint missing: %v", err)
	}
	if err := d.Steer(t.Context(), id, "hi"); !errors.Is(err, harness.ErrNotFound) {
		t.Fatalf("steer missing: %v", err)
	}
}

func testStopIdempotent(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	h := mustStart(t, d, "stop twice")
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatalf("Stop again: %v", err)
	}
}

func testInvalidSpec(t *testing.T, open func(t *testing.T) harness.Driver) {
	t.Helper()
	d := open(t)
	ws := t.TempDir()
	if _, err := d.Start(t.Context(), harness.Spec{Workspace: ws, Prompt: ""}); !errors.Is(err, harness.ErrInvalid) {
		t.Fatalf("empty prompt: %v", err)
	}
	if _, err := d.Start(t.Context(), harness.Spec{Workspace: "", Prompt: "x", Env: []string{apiKeyEnvPair()}}); !errors.Is(err, harness.ErrInvalid) {
		t.Fatalf("empty workspace: %v", err)
	}
	if _, err := d.Start(t.Context(), harness.Spec{Workspace: ws, Prompt: "x", Env: []string{"NOTPAIRS"}}); !errors.Is(err, harness.ErrInvalid) {
		t.Fatalf("malformed env: %v", err)
	}
	h := mustStart(t, d, "HOLD invalid steer")
	if err := d.Steer(t.Context(), h.ID, "  "); !errors.Is(err, harness.ErrInvalid) {
		t.Fatalf("empty steer: %v", err)
	}
}

func apiKeyEnvPair() string { return "ANTHROPIC_API_KEY=" + conformanceKey }

func specIn(t *testing.T, ws, prompt string) harness.Spec {
	t.Helper()
	return harness.Spec{
		Workspace: ws,
		Prompt:    prompt,
		Env: []string{
			apiKeyEnvPair(),
			"HOME=" + t.TempDir(),
		},
	}
}

func mustStart(t *testing.T, d harness.Driver, prompt string) harness.Handle {
	t.Helper()
	ws := t.TempDir()
	h, err := d.Start(t.Context(), specIn(t, ws, prompt))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background(), h.ID) })
	return h
}

func mustStream(t *testing.T, d harness.Driver, id harness.ID) <-chan harness.Event {
	t.Helper()
	ch, err := d.Stream(t.Context(), id)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	return ch
}

func waitFor(t *testing.T, ch <-chan harness.Event, timeout time.Duration, pred func([]harness.Event) bool) []harness.Event {
	t.Helper()
	deadline := time.After(timeout)
	var got []harness.Event
	for {
		if pred(got) {
			return got
		}
		select {
		case ev, ok := <-ch:
			if !ok {
				if pred(got) {
					return got
				}
				t.Fatalf("stream closed before predicate; got %#v", got)
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("timeout; got %#v", got)
		}
	}
}

func waitClosed(t *testing.T, ch <-chan harness.Event, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("stream did not close")
		}
	}
}

func hasKind(evs []harness.Event, k harness.EventKind) bool {
	for _, ev := range evs {
		if ev.Kind == k {
			return true
		}
	}
	return false
}

func tokenHas(evs []harness.Event, needle string) bool {
	for _, ev := range evs {
		if ev.Kind == harness.EventToken && strings.Contains(ev.Payload, needle) {
			return true
		}
	}
	return false
}

func payloadContains(evs []harness.Event, needle string) bool {
	for _, ev := range evs {
		if strings.Contains(ev.Payload, needle) {
			return true
		}
		for _, e := range ev.Effects {
			if strings.Contains(e.Diff, needle) || strings.Contains(e.Path, needle) {
				return true
			}
		}
	}
	return false
}

func buildFakeCLI(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src := filepath.Join(filepath.Dir(file), "claudecode", "testdata", "fakecli")
	bin := filepath.Join(t.TempDir(), "claude")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = src
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fakecli: %v\n%s", err, out)
	}
	return bin
}
