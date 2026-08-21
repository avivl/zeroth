package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func (c *cli) newRunsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "runs",
		Short: "List runs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := c.client().listRuns(cmd.Context())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tSTATUS\tPROMPT")
			for _, run := range list.Items {
				prompt := ""
				if run.Prompt != nil {
					prompt = *run.Prompt
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", run.Id, run.Status, prompt)
			}
			return tw.Flush()
		},
	}
}
