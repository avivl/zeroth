package main

import (
	"bytes"
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

func TestStubSubcommands(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"run", "attach", "bg"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cmd := newRoot()
			cmd.SetArgs([]string{name})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		})
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
	for _, want := range []string{"version", "run", "attach", "bg"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help missing %q: %s", want, got)
		}
	}
}
