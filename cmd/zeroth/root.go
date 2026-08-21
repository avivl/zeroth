package main

import (
	"fmt"

	"github.com/avivl/zeroth/internal/logging"
	"github.com/avivl/zeroth/internal/version"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func newRoot() *cobra.Command {
	var logLevel, logEncoding string
	cmd := &cobra.Command{
		Use:          "zeroth",
		Short:        "CLI and headless entry point for Zeroth",
		SilenceUsage: true,
		Version:      version.SHA(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log, err := logging.New(logging.Options{Level: logLevel, Encoding: logEncoding})
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
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "zap log level: debug, info, warn, error")
	cmd.PersistentFlags().StringVar(&logEncoding, "log-encoding", "console", "zap encoder: console or json")
	cmd.AddCommand(newVersionCmd(), newRunCmd(), newAttachCmd(), newBgCmd())
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

func newRunCmd() *cobra.Command {
	return stubCmd("run", "Run a headless session against local zerothd")
}

func newAttachCmd() *cobra.Command {
	return stubCmd("attach", "Attach to a running session")
}

func newBgCmd() *cobra.Command {
	return stubCmd("bg", "Background a running session")
}

func stubCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short + " (stub)",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.FromContext(cmd.Context()).Info("skeleton stub", zap.String("cmd", use))
			return nil
		},
	}
}
