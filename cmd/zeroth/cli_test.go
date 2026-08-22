package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
)

func TestCLIRunAttachBgRuns(t *testing.T) {
	t.Parallel()
	hs := cliServer(t)
	host := strings.TrimPrefix(hs.URL, "http://")

	var out bytes.Buffer
	run := newRoot()
	run.SetOut(&out)
	run.SetErr(io.Discard)
	run.SetArgs([]string{"--addr", host, "run", "cli-task"})
	if err := run.Execute(); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	id := strings.TrimSpace(out.String())
	if id == "" {
		t.Fatal("empty run id")
	}

	var listed bytes.Buffer
	runs := newRoot()
	runs.SetOut(&listed)
	runs.SetErr(io.Discard)
	runs.SetArgs([]string{"--addr", host, "runs"})
	if err := runs.Execute(); err != nil {
		t.Fatalf("runs: %v", err)
	}
	if !strings.Contains(listed.String(), id) {
		t.Fatalf("runs missing id:\n%s", listed.String())
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var attachOut bytes.Buffer
	attach := newRoot()
	attach.SetOut(&attachOut)
	attach.SetErr(io.Discard)
	attach.SetIn(bytes.NewBufferString("please hurry\n"))
	attach.SetArgs([]string{"--addr", host, "attach", "--last", "20", id})
	if err := attach.ExecuteContext(ctx); err != nil {
		t.Fatalf("attach: %v\n%s", err, attachOut.String())
	}
	text := attachOut.String()
	if !strings.Contains(text, "log") {
		t.Fatalf("attach missing log events:\n%s", text)
	}

	var bgOut bytes.Buffer
	bg := newRoot()
	bg.SetOut(&bgOut)
	bg.SetErr(io.Discard)
	bg.SetArgs([]string{"--addr", host, "bg", id})
	if err := bg.Execute(); err != nil {
		t.Fatalf("bg: %v\n%s", err, bgOut.String())
	}
}

func TestCLIAttachCancelDetaches(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: 15 * time.Millisecond,
		TokenCount:    80,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	host := strings.TrimPrefix(hs.URL, "http://")

	var out bytes.Buffer
	run := newRoot()
	run.SetOut(&out)
	run.SetErr(io.Discard)
	run.SetArgs([]string{"--addr", host, "run", "stay-alive"})
	if err := run.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	id := strings.TrimSpace(out.String())

	ctx, cancel := context.WithCancel(t.Context())
	var attachOut bytes.Buffer
	attach := newRoot()
	attach.SetOut(&attachOut)
	attach.SetErr(io.Discard)
	attach.SetIn(&bytes.Buffer{})
	attach.SetArgs([]string{"--addr", host, "attach", id})
	errc := make(chan error, 1)
	go func() { errc <- attach.ExecuteContext(ctx) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("attach cancel: %v\n%s", err, attachOut.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("attach did not return after cancel")
	}

	client := newAPIClient(host)
	got, err := client.getRun(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if runStatusTerminal(got.Status) {
		t.Fatalf("run died after detach: %s", got.Status)
	}
}

func TestCLIRetract(t *testing.T) {
	t.Parallel()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/runs/s_48/retract" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"s_48","agent_id":"a_1","status":"retracted","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","retract_reason":"bad patch"}`))
	}))
	t.Cleanup(hs.Close)
	host := strings.TrimPrefix(hs.URL, "http://")
	var out bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--addr", host, "retract", "s_48", "--reason", "bad patch"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("retract: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "s_48 retracted") {
		t.Fatalf("output %q", out.String())
	}
}

func cliServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: 5 * time.Millisecond,
		TokenCount:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs
}
