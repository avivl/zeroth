package harness

import (
	"fmt"
	"os"
	"strings"
)

const apiKeyEnv = "ANTHROPIC_API_KEY"

// ErrMissingAPIKey is returned when the operator has not set an API key.
var ErrMissingAPIKey = fmt.Errorf("harness: %s is not set", apiKeyEnv)

// Driver is the spike harness port. AuthReady is the one touchpoint:
// can we see an API key without OAuth?
type Driver interface {
	Name() string
	AuthReady() error
}

// ClaudeCode is a throwaway Claude Code harness driver.
type ClaudeCode struct{}

// NewClaudeCode returns the spike Claude Code driver.
func NewClaudeCode() *ClaudeCode { return &ClaudeCode{} }

// Name implements [Driver].
func (*ClaudeCode) Name() string { return "claudecode" }

var _ Driver = (*ClaudeCode)(nil)

// AuthReady reports whether ANTHROPIC_API_KEY is set. It does not
// call the network and it does not return the key.
func (*ClaudeCode) AuthReady() error {
	return APIKeyConfigured()
}

// APIKeyConfigured reports whether an Anthropic API key is present
// in the environment. The value is never returned.
func APIKeyConfigured() error {
	if strings.TrimSpace(os.Getenv(apiKeyEnv)) == "" {
		return ErrMissingAPIKey
	}
	return nil
}

// APIKeyConfiguredBool is the JSON-facing form of [APIKeyConfigured].
func APIKeyConfiguredBool() bool {
	return APIKeyConfigured() == nil
}
