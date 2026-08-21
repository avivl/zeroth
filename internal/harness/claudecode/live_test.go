package claudecode_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/harness/claudecode"
	"github.com/avivl/zeroth/internal/session"
)

// Live Claude Code is opt-in. CI uses the fake CLI. A real binary plus
// ANTHROPIC_API_KEY is required, and the operator must set
// ZEROTH_LIVE_HARNESS=1 so we do not spend credits accidentally.
func TestLiveClaudeCodeStream(t *testing.T) {
	if os.Getenv("ZEROTH_LIVE_HARNESS") != "1" {
		t.Skip("set ZEROTH_LIVE_HARNESS=1 to run a real claude subprocess")
	}
	if err := claudecode.APIKeyConfigured(); err != nil {
		t.Skip(err.Error())
	}
	bin := lookupLiveClaude()
	if bin == "" {
		t.Fatal("claude binary not found (CLAUDE_BIN, PATH, or ~/.local/bin/claude)")
	}
	if strings.Contains(bin, "fakecli") {
		t.Fatalf("refusing to run the live test against fake CLI %s", bin)
	}
	ver := claudeVersion(t, bin)
	t.Logf("claude binary: %s", bin)
	t.Logf("claude version: %s", ver)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	log := session.NewMemoryLog()
	sup, err := session.NewSupervisor(log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sup.Close)
	sid, err := sup.Start(ctx)
	if err != nil {
		t.Fatalf("session start: %v", err)
	}

	d := claudecode.New()
	ws := t.TempDir()
	h, err := d.Start(ctx, harness.Spec{
		Workspace: ws,
		Prompt:    "Reply with the single word ok. Do not call tools.",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = d.Stop(context.Background(), h.ID)
	}
	t.Cleanup(stop)

	ch, err := d.Stream(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}

	counts := map[harness.EventKind]int{}
	var tokens []string
	sawToken := false
	idle := time.NewTimer(time.Hour)
	idle.Stop()
	defer idle.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			counts[ev.Kind]++
			t.Logf("event kind=%s payload=%q effects=%s", ev.Kind, clip(ev.Payload, 200), effectsSummary(ev.Effects))
			switch ev.Kind {
			case harness.EventToken:
				if ev.Payload != "" {
					sawToken = true
					tokens = append(tokens, ev.Payload)
					if err := sup.EmitToken(ctx, sid, ev.Payload); err != nil {
						t.Fatalf("EmitToken: %v", err)
					}
				}
			case harness.EventToolCall:
				if err := sup.EmitToolCall(ctx, sid, ev.Payload); err != nil {
					t.Fatalf("EmitToolCall: %v", err)
				}
			case harness.EventEffects:
				body, err := json.Marshal(ev.Effects)
				if err != nil {
					t.Fatal(err)
				}
				if err := sup.ProposePlan(ctx, sid, string(body)); err != nil {
					t.Fatalf("ProposePlan: %v", err)
				}
			case harness.EventExited:
				goto done
			}
			if sawToken && !stopped {
				idle.Reset(2 * time.Second)
			}
		case <-idle.C:
			// Live claude with stream-json stdin stays up after the first
			// result so Steer can write another turn. Stop once the first
			// burst of tokens has gone quiet.
			stop()
		case <-ctx.Done():
			t.Fatalf("live run timed out after events %#v tokens=%q", counts, strings.Join(tokens, ""))
		}
	}

done:
	t.Logf("event counts: %v", counts)
	t.Logf("concatenated tokens: %q", strings.Join(tokens, ""))
	if !sawToken {
		t.Fatal("live run produced no tokens")
	}

	evs, err := sup.Events(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	var logTok, logTool bool
	for _, ev := range evs {
		switch ev.Type {
		case session.EventToken:
			logTok = true
		case session.EventToolCall:
			logTool = true
		}
	}
	if !logTok {
		t.Fatalf("session log missing token events: %+v", evs)
	}
	t.Logf("session log: events=%d token=%v tool_call=%v", len(evs), logTok, logTool)
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "..."
}

func effectsSummary(effects []harness.Effect) string {
	if len(effects) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(effects)
	if err != nil {
		return "<marshal error>"
	}
	return clip(string(raw), 240)
}

func lookupLiveClaude() string {
	if p := strings.TrimSpace(os.Getenv("CLAUDE_BIN")); p != "" {
		return p
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(home, ".local", "bin", "claude")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func claudeVersion(t *testing.T, bin string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return "(version failed: " + err.Error() + ")"
	}
	return strings.TrimSpace(string(out))
}
