package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func (c *cli) newRetractCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "retract <run-id>",
		Short: "Close a run's pull request and record the retraction on the tracker issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reason = strings.TrimSpace(reason)
			if reason == "" {
				return fmt.Errorf("zeroth retract: --reason is required")
			}
			run, err := c.client().retract(cmd.Context(), args[0], reason)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s retracted\n", run.Id)
			return err
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why this run's output is being disavowed (required)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}
