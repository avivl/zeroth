package claudecode_test

import (
	"os"
	"testing"

	"github.com/avivl/zeroth/internal/harness"
	"github.com/avivl/zeroth/internal/harness/claudecode"
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
	d := claudecode.New()
	ws := t.TempDir()
	h, err := d.Start(t.Context(), harness.Spec{
		Workspace: ws,
		Prompt:    "Reply with the single word ok. Do not call tools.",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop(t.Context(), h.ID) })
	ch, err := d.Stream(t.Context(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	saw := false
	for ev := range ch {
		if ev.Kind == harness.EventToken && ev.Payload != "" {
			saw = true
		}
		if ev.Kind == harness.EventExited {
			break
		}
	}
	if !saw {
		t.Fatal("live run produced no tokens")
	}
}
