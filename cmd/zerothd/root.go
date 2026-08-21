package main

import (
	"github.com/avivl/zeroth/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRoot(d deps) (*cobra.Command, *viper.Viper) {
	v := viper.New()
	cmd := &cobra.Command{
		Use:          "zerothd",
		Short:        "Zeroth daemon: local control plane",
		Long:         "zerothd is the local, single-player control plane. It binds 127.0.0.1:8420 by default.",
		SilenceUsage: true,
		Version:      version.SHA(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig(cmd, v)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon(cmd.Context(), configFrom(v), d)
		},
	}
	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.CompletionOptions.DisableDefaultCmd = true
	registerFlags(cmd)
	return cmd, v
}
