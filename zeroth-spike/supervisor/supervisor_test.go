package supervisor_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/eventlog"
	"github.com/avivl/zeroth/zeroth-spike/supervisor"
)

func TestFakeAgentWritesTokens(t *testing.T) {
	t.Parallel()
	log := openLog(t)
	sup := supervisor.New(log)
	t.Cleanup(sup.Close)

	id, err := sup.Start(&supervisor.FakeAgent{Interval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := log.ReplayLast(context.Background(), id, 50)
		if err != nil {
			t.Fatal(err)
		}
		tokens := 0
		for _, ev := range got {
			if ev.Type == eventlog.TypeToken {
				tokens++
			}
		}
		if tokens >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected token events from fake agent")
}

func TestCmdAgentEcho(t *testing.T) {
	t.Parallel()
	path, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("echo not on PATH")
	}
	log := openLog(t)
	sup := supervisor.New(log)
	t.Cleanup(sup.Close)

	id, err := sup.Start(&supervisor.CmdAgent{Path: path, Args: []string{"hello-from-cmd"}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := log.ReplayLast(context.Background(), id, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range got {
			if ev.Type == eventlog.TypeToken && strings.Contains(ev.Payload, "hello-from-cmd") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected echo payload in event log")
}

func TestBackgroundAndStop(t *testing.T) {
	t.Parallel()
	log := openLog(t)
	sup := supervisor.New(log)
	t.Cleanup(sup.Close)

	id, err := sup.Start(&supervisor.FakeAgent{Interval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := sup.Background(id); err != nil {
		t.Fatal(err)
	}
	info, found, err := sup.Lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !found || info.Foreground {
		t.Fatalf("after bg: %+v found=%v", info, found)
	}
	if err := sup.Stop(id); err != nil {
		t.Fatal(err)
	}
	got, err := log.ReplayLast(context.Background(), id, 50)
	if err != nil {
		t.Fatal(err)
	}
	var sawBG, sawStop bool
	for _, ev := range got {
		if ev.Type == eventlog.TypeBackgrounded {
			sawBG = true
		}
		if ev.Type == eventlog.TypeStopped {
			sawStop = true
		}
	}
	if !sawBG || !sawStop {
		t.Fatalf("events missing bg/stop: %+v", got)
	}
}

func TestClaudeAgentConstructed(t *testing.T) {
	t.Parallel()
	a := supervisor.ClaudePromptAgent()
	if a.Path != "claude" {
		t.Fatalf("path = %q", a.Path)
	}
	if len(a.Args) < 1 || a.Args[0] != "-p" {
		t.Fatalf("args = %v, want claude -p", a.Args)
	}
}

func openLog(t *testing.T) *eventlog.Log {
	t.Helper()
	log, err := eventlog.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}
