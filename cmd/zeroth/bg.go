package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *cli) newBgCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bg <run-id>",
		Short: "Background a run with the default completion contract",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, err := c.client().background(cmd.Context(), args[0])
			if err != nil && !isConflict(err) {
				return fmt.Errorf("zeroth bg: %w", err)
			}
			id := args[0]
			if err == nil {
				id = string(run.Id)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), id)
			return err
		},
	}
}
