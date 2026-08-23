package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
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

func TestCLIRetract(t *testing.T) {
	t.Parallel()
	reason := "Apply overwrote README.md instead of patching it."
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/runs/s_48/retract" {
			http.NotFound(w, r)
			return
		}
		var req gen.RetractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Reason != reason {
			http.Error(w, "reason", http.StatusBadRequest)
			return
		}
		now := time.Now().UTC()
		_ = json.NewEncoder(w).Encode(gen.Run{
			Id:            "s_48",
			AgentId:       "a_default",
			Status:        gen.RunStatusRetracted,
			RetractReason: &reason,
			RetractedAt:   &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}))
	t.Cleanup(hs.Close)
	host := strings.TrimPrefix(hs.URL, "http://")

	var out bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--addr", host, "retract", "s_48", "--reason", reason})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("retract: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "s_48 retracted") {
		t.Fatalf("output %q", out.String())
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
