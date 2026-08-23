package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/avivl/zeroth/internal/logging"
	"github.com/avivl/zeroth/internal/version"
	"github.com/spf13/cobra"
)

const defaultDaemonAddr = "127.0.0.1:8420"

type cli struct {
	addr        string
	logLevel    string
	logEncoding string
}

func newRoot() *cobra.Command {
	c := &cli{}
	cmd := &cobra.Command{
		Use:          "zeroth",
		Short:        "CLI and headless entry point for Zeroth",
		SilenceUsage: true,
		Version:      version.SHA(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log, err := logging.New(logging.Options{Level: c.logLevel, Encoding: c.logEncoding})
			if err != nil {
				return fmt.Errorf("zeroth logger: %w", err)
			}
			cmd.SetContext(logging.WithLogger(cmd.Context(), log))
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			_ = logging.FromContext(cmd.Context()).Sync()
		},
	}
	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.PersistentFlags().StringVar(&c.logLevel, "log-level", "info", "zap log level: debug, info, warn, error")
	cmd.PersistentFlags().StringVar(&c.logEncoding, "log-encoding", "console", "zap encoder: console or json")
	cmd.PersistentFlags().StringVar(&c.addr, "addr", envOr("ZEROTH_ADDR", defaultDaemonAddr), "zerothd address (ZEROTH_ADDR)")
	cmd.AddCommand(
		newVersionCmd(),
		c.newRunCmd(),
		c.newAttachCmd(),
		c.newBgCmd(),
		c.newRunsCmd(),
		c.newRetractCmd(),
		c.newVerifyCmd(),
		c.newRejectCmd(),
	)
	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the build SHA",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.SHA())
			return err
		},
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (c *cli) client() *apiClient {
	return newAPIClient(c.addr)
}
