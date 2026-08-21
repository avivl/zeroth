package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *cli) newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <run-id|artifact>",
		Short: "Verify a run or artifact (stub; lands in M4)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "zeroth verify: not implemented until M4 (%s)\n", args[0])
			return err
		},
	}
}
