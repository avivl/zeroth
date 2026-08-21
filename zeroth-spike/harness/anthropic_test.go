package harness_test

import (
	"errors"
	"testing"

	"github.com/avivl/zeroth/zeroth-spike/harness"
)

func TestClaudeCodeName(t *testing.T) {
	t.Parallel()
	if got := harness.NewClaudeCode().Name(); got != "claudecode" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestAPIKeyConfigured(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	if err := harness.APIKeyConfigured(); !errors.Is(err, harness.ErrMissingAPIKey) {
		t.Fatalf("empty: err = %v", err)
	}
	if harness.APIKeyConfiguredBool() {
		t.Fatal("empty key reported configured")
	}
	if err := harness.NewClaudeCode().AuthReady(); !errors.Is(err, harness.ErrMissingAPIKey) {
		t.Fatalf("AuthReady empty: err = %v", err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-not-a-real-key")
	if err := harness.APIKeyConfigured(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !harness.APIKeyConfiguredBool() {
		t.Fatal("set key reported missing")
	}
	if err := harness.NewClaudeCode().AuthReady(); err != nil {
		t.Fatalf("AuthReady set: %v", err)
	}
}
