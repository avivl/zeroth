package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	defaultAddr         = "127.0.0.1:8420"
	defaultDBPath       = "zeroth.db"
	defaultDockerSocket = "/var/run/docker.sock"
	defaultLogLevel     = "info"
	defaultLogEncoding  = "console"
)

// Config is zerothd startup configuration. Precedence is flags, then env,
// then the yaml config file, then defaults.
type Config struct {
	Addr         string
	DBPath       string
	DockerSocket string
	LogLevel     string
	LogEncoding  string
	SigningKey   string
	ConfigFile   string

	LinearAPIKey        string
	LinearEndpoint      string
	LinearAgentUser     string
	LinearTeamID        string
	LinearProjectID     string
	LinearPollInterval  time.Duration
	LinearWebhookSecret string
	// LinearAuthStyle is "personal" (raw API key, default) or "oauth"
	// (Bearer actor token). See linear.AuthStyle.
	LinearAuthStyle string
}

func registerFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("config", "", "path to yaml config file (ZEROTH_CONFIG)")
	f.String("addr", defaultAddr, "bind address for the local control plane (ZEROTH_ADDR)")
	f.String("db-path", defaultDBPath, "sqlite database path (ZEROTH_DB_PATH)")
	f.String("docker-socket", defaultDockerSocket, "docker daemon unix socket (ZEROTH_DOCKER_SOCKET)")
	f.String("log-level", defaultLogLevel, "zap log level: debug, info, warn, error (ZEROTH_LOG_LEVEL)")
	f.String("log-encoding", defaultLogEncoding, "zap encoder: console or json (ZEROTH_LOG_ENCODING)")
	f.String("signing-key", "", "path to secp256k1 signing key file (ZEROTH_SIGNING_KEY)")
	f.String("linear-api-key", "", "Linear API key for assign-to-Zeroth (ZEROTH_LINEAR_API_KEY)")
	f.String("linear-endpoint", "", "Linear GraphQL endpoint (ZEROTH_LINEAR_ENDPOINT)")
	f.String("linear-agent-user", "", "Linear user id of the Zeroth agent identity (ZEROTH_LINEAR_AGENT_USER)")
	f.String("linear-team-id", "", "optional Linear team id filter (ZEROTH_LINEAR_TEAM_ID)")
	f.String("linear-project-id", "", "optional Linear project id filter (ZEROTH_LINEAR_PROJECT_ID)")
	f.String("linear-poll-interval", "15s", "assignment poll interval (ZEROTH_LINEAR_POLL_INTERVAL)")
	f.String("linear-webhook-secret", "", "opt-in Linear webhook HMAC secret (ZEROTH_LINEAR_WEBHOOK_SECRET)")
	f.String("linear-auth-style", "personal", "Linear auth: personal (API key) or oauth (Bearer actor token) (ZEROTH_LINEAR_AUTH_STYLE)")
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("addr", defaultAddr)
	v.SetDefault("db-path", defaultDBPath)
	v.SetDefault("docker-socket", defaultDockerSocket)
	v.SetDefault("log-level", defaultLogLevel)
	v.SetDefault("log-encoding", defaultLogEncoding)
	v.SetDefault("signing-key", "")
	v.SetDefault("linear-api-key", "")
	v.SetDefault("linear-endpoint", "")
	v.SetDefault("linear-agent-user", "")
	v.SetDefault("linear-team-id", "")
	v.SetDefault("linear-project-id", "")
	v.SetDefault("linear-poll-interval", "15s")
	v.SetDefault("linear-webhook-secret", "")
	v.SetDefault("linear-auth-style", "personal")
}

func loadConfig(cmd *cobra.Command, v *viper.Viper) error {
	setDefaults(v)
	v.SetEnvPrefix("ZEROTH")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	cfgFlag := strings.TrimSpace(flagOrEmpty(cmd, "config"))
	if cfgFlag == "" {
		cfgFlag = strings.TrimSpace(os.Getenv("ZEROTH_CONFIG"))
	}
	if cfgFlag != "" {
		v.SetConfigFile(cfgFlag)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("zerothd config read %s: %w", cfgFlag, err)
		}
	} else {
		v.SetConfigName("zeroth")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		if err := v.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) {
				return fmt.Errorf("zerothd config: %w", err)
			}
		}
	}

	for _, name := range []string{
		"addr", "db-path", "docker-socket", "log-level", "log-encoding", "signing-key",
		"linear-api-key", "linear-endpoint", "linear-agent-user", "linear-team-id",
		"linear-project-id", "linear-poll-interval", "linear-webhook-secret",
		"linear-auth-style",
	} {
		if !cmd.Flags().Changed(name) {
			continue
		}
		val := strings.TrimSpace(cmd.Flags().Lookup(name).Value.String())
		if val == "" {
			continue
		}
		v.Set(name, val)
	}
	return nil
}

func flagOrEmpty(cmd *cobra.Command, name string) string {
	f := cmd.Flags().Lookup(name)
	if f == nil || !cmd.Flags().Changed(name) {
		return ""
	}
	return f.Value.String()
}

func configFrom(v *viper.Viper) Config {
	poll := v.GetDuration("linear-poll-interval")
	return Config{
		Addr:                strings.TrimSpace(v.GetString("addr")),
		DBPath:              strings.TrimSpace(v.GetString("db-path")),
		DockerSocket:        strings.TrimSpace(v.GetString("docker-socket")),
		LogLevel:            strings.TrimSpace(v.GetString("log-level")),
		LogEncoding:         strings.TrimSpace(v.GetString("log-encoding")),
		SigningKey:          strings.TrimSpace(v.GetString("signing-key")),
		ConfigFile:          v.ConfigFileUsed(),
		LinearAPIKey:        strings.TrimSpace(v.GetString("linear-api-key")),
		LinearEndpoint:      strings.TrimSpace(v.GetString("linear-endpoint")),
		LinearAgentUser:     strings.TrimSpace(v.GetString("linear-agent-user")),
		LinearTeamID:        strings.TrimSpace(v.GetString("linear-team-id")),
		LinearProjectID:     strings.TrimSpace(v.GetString("linear-project-id")),
		LinearPollInterval:  poll,
		LinearWebhookSecret: strings.TrimSpace(v.GetString("linear-webhook-secret")),
		LinearAuthStyle:     strings.TrimSpace(v.GetString("linear-auth-style")),
	}
}
