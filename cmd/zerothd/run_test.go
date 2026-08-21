package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunDaemonLogsJSONAndProbes(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var probed string
	cfg := Config{
		Addr:         "127.0.0.1:7999",
		DBPath:       "zeroth.db",
		DockerSocket: "/tmp/docker.sock",
		LogLevel:     "info",
		LogEncoding:  "json",
	}
	err := runDaemon(t.Context(), cfg, deps{
		writer: &buf,
		probe: func(_ context.Context, socket string) error {
			probed = socket
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runDaemon: %v", err)
	}
	if probed != cfg.DockerSocket {
		t.Fatalf("probed %q, want %s", probed, cfg.DockerSocket)
	}
	out := buf.String()
	if !strings.Contains(out, "skeleton stub") {
		t.Fatalf("missing stub log: %s", out)
	}
	if !strings.Contains(out, "docker socket reachable") {
		t.Fatalf("missing probe log: %s", out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var rec struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("json line %q: %v", line, err)
		}
		if rec.Msg == "" {
			t.Fatalf("empty msg in %q", line)
		}
	}
}

func TestRunDaemonInvalidLevel(t *testing.T) {
	t.Parallel()
	err := runDaemon(t.Context(), Config{LogLevel: "fatal", LogEncoding: "json"}, deps{
		probe: func(context.Context, string) error { return nil },
	})
	if err == nil {
		t.Fatal("expected logger error")
	}
}

func TestRootHelp(t *testing.T) {
	t.Parallel()
	cmd, _ := newRoot(deps{})
	got := cmd.UsageString()
	if !strings.Contains(got, "--addr") || !strings.Contains(got, "--db-path") {
		t.Fatalf("help missing flags: %s", got)
	}
}
