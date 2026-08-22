package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/tracker/linear"
)

func TestRunDaemonLogsJSONAndProbes(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var probed string
	cfg := Config{
		Addr:         "127.0.0.1:7999",
		DBPath:       filepath.Join(t.TempDir(), "zeroth.db"),
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
		serve: func(_ context.Context, addr string, h http.Handler) error {
			if h == nil {
				t.Fatal("nil handler")
			}
			if addr != cfg.Addr {
				t.Fatalf("serve addr %q, want %s", addr, cfg.Addr)
			}
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
	if !strings.Contains(out, "zerothd listening") {
		t.Fatalf("missing listening log: %s", out)
	}
	if strings.Contains(out, "skeleton stub") {
		t.Fatalf("stub log still present: %s", out)
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

func TestRunDaemonLogsLinearPollError(t *testing.T) {
	t.Parallel()
	fake := linear.NewFake()
	gql := httptest.NewServer(fake)
	t.Cleanup(gql.Close)

	var buf safeBuffer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	dir := t.TempDir()
	cfg := Config{
		Addr:         "127.0.0.1:0",
		DBPath:       filepath.Join(dir, "zeroth.db"),
		DockerSocket: "/tmp/docker.sock",
		LogLevel:     "info",
		LogEncoding:  "json",
		// Skip the D-Bus keyring probe. In CI it can take seconds and
		// starve the wait for the first poll error.
		SigningKey:         filepath.Join(dir, "zeroth.keys"),
		LinearAPIKey:       "not-the-key",
		LinearEndpoint:     gql.URL,
		LinearAgentUser:    fake.AgentUserID,
		LinearPollInterval: 20 * time.Millisecond,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- runDaemon(ctx, cfg, deps{
			writer: &buf,
			probe:  func(context.Context, string) error { return nil },
			serve: func(ctx context.Context, _ string, h http.Handler) error {
				if h == nil {
					t.Error("nil handler")
				}
				<-ctx.Done()
				return nil
			},
		})
	}()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out := buf.String()
		if strings.Contains(out, `"msg":"tracker linear poll"`) && strings.Contains(out, `"level":"error"`) {
			cancel()
			if err := <-errc; err != nil {
				t.Fatalf("runDaemon: %v", err)
			}
			if !strings.Contains(out, "unauthorized") {
				t.Fatalf("poll error missing unauthorized: %s", out)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-errc
	t.Fatalf("missing info-visible poll error log: %s", buf.String())
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
