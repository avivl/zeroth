package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	t.Parallel()
	cmd := newRoot()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got == "" {
		t.Fatal("empty version")
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()
	cmd := newRoot()
	cmd.SetArgs([]string{"not-a-command"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestHelpListsSubcommands(t *testing.T) {
	t.Parallel()
	cmd := newRoot()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"version", "run", "attach", "bg", "runs", "verify", "reject", "retract"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help missing %q: %s", want, got)
		}
	}
}

func TestVerifyRequiresRunID(t *testing.T) {
	t.Parallel()
	cmd := newRoot()
	cmd.SetArgs([]string{"verify"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRetractRequiresReason(t *testing.T) {
	t.Parallel()
	cmd := newRoot()
	cmd.SetArgs([]string{"retract", "s_1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRetractWhitespaceReason(t *testing.T) {
	t.Parallel()
	cmd := newRoot()
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"retract", "s_1", "--reason", "   "})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRetainEvent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		last, id string
		want     bool
	}{
		{"", "1", true},
		{"1", "1", false},
		{"1", "2", true},
		{"9", "10", true},
		{"10", "9", false},
		{"abc", "abc", false},
		{"abc", "abd", true},
	}
	for _, tc := range cases {
		if got := retainEvent(tc.last, tc.id); got != tc.want {
			t.Fatalf("retainEvent(%q, %q) = %v, want %v", tc.last, tc.id, got, tc.want)
		}
	}
}

func TestHttpBase(t *testing.T) {
	t.Parallel()
	if got := httpBase("127.0.0.1:8420"); got != "http://127.0.0.1:8420" {
		t.Fatalf("got %q", got)
	}
	if got := httpBase("http://127.0.0.1:9/"); got != "http://127.0.0.1:9" {
		t.Fatalf("got %q", got)
	}
	if got := wsBase("http://127.0.0.1:8420"); got != "ws://127.0.0.1:8420" {
		t.Fatalf("ws %q", got)
	}
	_ = context.Background()
}
