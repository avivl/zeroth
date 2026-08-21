package claudecode

import (
	"errors"
	"testing"
)

func TestAPIKeyConfigured(t *testing.T) {
	t.Setenv(apiKeyEnv, "")
	if err := APIKeyConfigured(); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("empty: err = %v", err)
	}

	t.Setenv(apiKeyEnv, "test-not-a-real-key")
	if err := APIKeyConfigured(); err != nil {
		t.Fatalf("set: %v", err)
	}
}

func TestAPIKeyFromEnvListPrefersSpec(t *testing.T) {
	t.Setenv(apiKeyEnv, "from-process")
	got := apiKeyFromEnvList([]string{apiKeyEnv + "=from-spec"})
	if got != "from-spec" {
		t.Fatalf("got %q", got)
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()
	key := "sk-ant-abcdefgh"
	if got := redact("pre "+key+" post", key); got != "pre [redacted] post" {
		t.Fatalf("got %q", got)
	}
	if got := redact("none", key); got != "none" {
		t.Fatalf("untouched %q", got)
	}
	if got := redact("ab", "ab"); got != "ab" {
		t.Fatal("short key should not redact")
	}
}
