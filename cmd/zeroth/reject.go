package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func (c *cli) newRejectCmd() *cobra.Command {
	var comment string
	cmd := &cobra.Command{
		Use:   "reject <plan-id>",
		Short: "Reject a plan with a comment that feeds the next draft",
		Long: `Reject a pending plan and send the comment back into the next draft.

The daemon stores the comment on the plan, posts it on the tracker issue,
appends it to the run prompt, and starts another plan-generation turn.
A later assign-to-Zeroth run also reads the issue comment thread, so the
correction survives un-assign.

This is POST /plans/{id}/request-changes. There is no apply without a
new approval.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			comment = strings.TrimSpace(comment)
			if comment == "" {
				return fmt.Errorf("zeroth reject: --comment is required")
			}
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("zeroth reject: empty plan id")
			}
			plan, err := c.client().requestChanges(cmd.Context(), id, comment)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), plan.Id)
			return err
		},
	}
	cmd.Flags().StringVar(&comment, "comment", "", "operator feedback the next plan must address")
	return cmd
}
