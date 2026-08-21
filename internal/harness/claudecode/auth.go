package claudecode

import (
	"fmt"
	"os"
	"strings"
)

const apiKeyEnv = "ANTHROPIC_API_KEY"

// ErrMissingAPIKey is returned when the operator has not set an API key.
var ErrMissingAPIKey = fmt.Errorf("harness claudecode: %s is not set", apiKeyEnv)

// APIKeyConfigured reports whether an Anthropic API key is present
// in the environment. The value is never returned.
func APIKeyConfigured() error {
	if strings.TrimSpace(os.Getenv(apiKeyEnv)) == "" {
		return ErrMissingAPIKey
	}
	return nil
}

func apiKeyFromEnvList(env []string) string {
	key := ""
	for _, e := range env {
		name, val, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if name == apiKeyEnv {
			key = val
		}
	}
	if strings.TrimSpace(key) != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv(apiKeyEnv))
}

func redact(s, key string) string {
	if key == "" || len(key) < 8 || s == "" {
		return s
	}
	if !strings.Contains(s, key) {
		return s
	}
	return strings.ReplaceAll(s, key, "[redacted]")
}
