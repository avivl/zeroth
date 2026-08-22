package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func unsetenv(t *testing.T, key string) {
	t.Helper()
	orig, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if !wasSet {
			_ = os.Unsetenv(key)
			return
		}
		_ = os.Setenv(key, orig)
	})
}

func TestConfigDefaults(t *testing.T) {
	got := loadFrom(t, nil, nil)
	if got.Addr != defaultAddr {
		t.Fatalf("addr = %q, want %s", got.Addr, defaultAddr)
	}
	if got.DBPath != defaultDBPath {
		t.Fatalf("db-path = %q, want %s", got.DBPath, defaultDBPath)
	}
	if got.DockerSocket != defaultDockerSocket {
		t.Fatalf("docker-socket = %q, want %s", got.DockerSocket, defaultDockerSocket)
	}
	if got.LogLevel != defaultLogLevel {
		t.Fatalf("log-level = %q, want %s", got.LogLevel, defaultLogLevel)
	}
	if got.LogEncoding != defaultLogEncoding {
		t.Fatalf("log-encoding = %q, want %s", got.LogEncoding, defaultLogEncoding)
	}
}

func TestConfigEnvOverridesDefault(t *testing.T) {
	got := loadFrom(t, nil, map[string]string{
		"ZEROTH_ADDR":          "127.0.0.1:9000",
		"ZEROTH_DB_PATH":       "/tmp/x.db",
		"ZEROTH_DOCKER_SOCKET": "/tmp/docker.sock",
		"ZEROTH_LOG_LEVEL":     "debug",
		"ZEROTH_LOG_ENCODING":  "json",
		"ZEROTH_SIGNING_KEY":   "/tmp/key",
	})
	if got.Addr != "127.0.0.1:9000" {
		t.Fatalf("addr = %q", got.Addr)
	}
	if got.DBPath != "/tmp/x.db" {
		t.Fatalf("db-path = %q", got.DBPath)
	}
	if got.DockerSocket != "/tmp/docker.sock" {
		t.Fatalf("docker-socket = %q", got.DockerSocket)
	}
	if got.LogLevel != "debug" {
		t.Fatalf("log-level = %q", got.LogLevel)
	}
	if got.LogEncoding != "json" {
		t.Fatalf("log-encoding = %q", got.LogEncoding)
	}
	if got.SigningKey != "/tmp/key" {
		t.Fatalf("signing-key = %q", got.SigningKey)
	}
}

func TestConfigFlagWinsOverEnv(t *testing.T) {
	got := loadFrom(t, []string{"--addr", "127.0.0.1:1"}, map[string]string{
		"ZEROTH_ADDR": "127.0.0.1:2",
	})
	if got.Addr != "127.0.0.1:1" {
		t.Fatalf("addr = %q, want flag 127.0.0.1:1", got.Addr)
	}
}

func TestConfigWhitespaceFlagIgnored(t *testing.T) {
	got := loadFrom(t, []string{"--addr", "  "}, map[string]string{
		"ZEROTH_ADDR": "127.0.0.1:9",
	})
	if got.Addr != "127.0.0.1:9" {
		t.Fatalf("addr = %q, want env 127.0.0.1:9", got.Addr)
	}
}

func TestConfigFileThenEnvThenFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zeroth.yaml")
	body := []byte("addr: 127.0.0.1:7000\ndb-path: from-file.db\nlog-level: warn\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fromFile := loadFrom(t, []string{"--config", path}, nil)
	if fromFile.Addr != "127.0.0.1:7000" {
		t.Fatalf("file addr = %q", fromFile.Addr)
	}
	if fromFile.DBPath != "from-file.db" {
		t.Fatalf("file db-path = %q", fromFile.DBPath)
	}
	if fromFile.LogLevel != "warn" {
		t.Fatalf("file log-level = %q", fromFile.LogLevel)
	}

	fromEnv := loadFrom(t, []string{"--config", path}, map[string]string{
		"ZEROTH_ADDR":      "127.0.0.1:7100",
		"ZEROTH_LOG_LEVEL": "error",
	})
	if fromEnv.Addr != "127.0.0.1:7100" {
		t.Fatalf("env addr = %q, want override of file", fromEnv.Addr)
	}
	if fromEnv.DBPath != "from-file.db" {
		t.Fatalf("db-path should stay from file, got %q", fromEnv.DBPath)
	}
	if fromEnv.LogLevel != "error" {
		t.Fatalf("env log-level = %q", fromEnv.LogLevel)
	}

	fromFlag := loadFrom(t, []string{"--config", path, "--addr", "127.0.0.1:7200"}, map[string]string{
		"ZEROTH_ADDR": "127.0.0.1:7100",
	})
	if fromFlag.Addr != "127.0.0.1:7200" {
		t.Fatalf("flag addr = %q, want override of env and file", fromFlag.Addr)
	}
}

func TestConfigLinearEnv(t *testing.T) {
	got := loadFrom(t, nil, map[string]string{
		"ZEROTH_LINEAR_AGENT_USER":    "user_agent",
		"ZEROTH_LINEAR_TEAM_ID":       "team_1",
		"ZEROTH_LINEAR_PROJECT_ID":    "proj_1",
		"ZEROTH_LINEAR_POLL_INTERVAL": "2s",
		"ZEROTH_LINEAR_ENDPOINT":      "http://127.0.0.1:9/graphql",
	})
	if got.LinearAgentUser != "user_agent" {
		t.Fatalf("agent user = %q", got.LinearAgentUser)
	}
	if got.LinearTeamID != "team_1" || got.LinearProjectID != "proj_1" {
		t.Fatalf("team/project = %q %q", got.LinearTeamID, got.LinearProjectID)
	}
	if got.LinearPollInterval != 2*time.Second {
		t.Fatalf("poll = %s", got.LinearPollInterval)
	}
	if got.LinearEndpoint != "http://127.0.0.1:9/graphql" {
		t.Fatalf("endpoint = %q", got.LinearEndpoint)
	}
	if got.LinearAPIKey != "" {
		t.Fatal("api key should stay empty in tests")
	}
}

func TestConfigMissingExplicitFile(t *testing.T) {
	cmd, _ := newRoot(deps{})
	cmd.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")})
	cmd.RunE = func(*cobra.Command, []string) error { return nil }
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing --config")
	}
}

func loadFrom(t *testing.T, args []string, env map[string]string) Config {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, k := range []string{
		"ZEROTH_ADDR", "ZEROTH_DB_PATH", "ZEROTH_DOCKER_SOCKET",
		"ZEROTH_LOG_LEVEL", "ZEROTH_LOG_ENCODING", "ZEROTH_SIGNING_KEY", "ZEROTH_CONFIG",
		"ZEROTH_LINEAR_API_KEY", "ZEROTH_LINEAR_ENDPOINT", "ZEROTH_LINEAR_AGENT_USER",
		"ZEROTH_LINEAR_TEAM_ID", "ZEROTH_LINEAR_PROJECT_ID", "ZEROTH_LINEAR_POLL_INTERVAL",
		"ZEROTH_LINEAR_WEBHOOK_SECRET",
	} {
		if val, ok := env[k]; ok {
			t.Setenv(k, val)
		} else {
			unsetenv(t, k)
		}
	}
	cmd, v := newRoot(deps{})
	if args == nil {
		args = []string{}
	}
	cmd.SetArgs(args)
	var got Config
	cmd.RunE = func(*cobra.Command, []string) error {
		got = configFrom(v)
		return nil
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return got
}
