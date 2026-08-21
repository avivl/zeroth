package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

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
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("addr", defaultAddr)
	v.SetDefault("db-path", defaultDBPath)
	v.SetDefault("docker-socket", defaultDockerSocket)
	v.SetDefault("log-level", defaultLogLevel)
	v.SetDefault("log-encoding", defaultLogEncoding)
	v.SetDefault("signing-key", "")
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

	for _, name := range []string{"addr", "db-path", "docker-socket", "log-level", "log-encoding", "signing-key"} {
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
	return Config{
		Addr:         strings.TrimSpace(v.GetString("addr")),
		DBPath:       strings.TrimSpace(v.GetString("db-path")),
		DockerSocket: strings.TrimSpace(v.GetString("docker-socket")),
		LogLevel:     strings.TrimSpace(v.GetString("log-level")),
		LogEncoding:  strings.TrimSpace(v.GetString("log-encoding")),
		SigningKey:   strings.TrimSpace(v.GetString("signing-key")),
		ConfigFile:   v.ConfigFileUsed(),
	}
}
