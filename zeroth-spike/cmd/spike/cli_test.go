package main

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCrossProcessAttach(t *testing.T) {
	bin := buildSpike(t)
	addr := freeAddr(t)
	db := filepath.Join(t.TempDir(), "spike.db")
	fixtures := t.TempDir()

	serve := exec.Command(bin, "serve", "-addr", addr, "-db", db, "-fixtures", fixtures)
	serve.Stderr = os.Stderr
	if err := serve.Start(); err != nil {
		t.Fatalf("serve start: %v", err)
	}
	t.Cleanup(func() {
		_ = serve.Process.Kill()
		_, _ = serve.Process.Wait()
	})
	waitHealth(t, addr)

	run := exec.Command(bin, "run", "-addr", addr, "-agent", "fake", "-interval-ms", "5")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		t.Fatal("empty session id")
	}

	attach := exec.Command(bin, "attach", "-addr", addr, "-last", "10", "-max-events", "2", id)
	got, err := attach.CombinedOutput()
	if err != nil {
		t.Fatalf("attach: %v\n%s", err, got)
	}
	text := string(got)
	if !strings.Contains(text, "caught_up") {
		t.Fatalf("attach missing caught_up:\n%s", text)
	}
	if !strings.Contains(text, "live") || !strings.Contains(text, "type=token") {
		t.Fatalf("attach missing live token:\n%s", text)
	}

	bg := exec.Command(bin, "bg", "-addr", addr, id)
	if out, err := bg.CombinedOutput(); err != nil {
		t.Fatalf("bg: %v\n%s", err, out)
	}
}

func buildSpike(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "spike")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitHealth(t *testing.T, addr string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	url := "http://" + addr + "/health"
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		res, err := client.Get(url)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
			last = err
		} else {
			last = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("health: %v", last)
}
