package main

import (
	"context"
	"fmt"
	"time"

	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"github.com/spf13/cobra"
)

func (c *cli) newAttachCmd() *cobra.Command {
	var last int
	cmd := &cobra.Command{
		Use:   "attach <run-id>",
		Short: "Replay recent events, then live-tail; type to steer. Ctrl-C detaches",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id := args[0]
			client := c.client()
			run, err := client.getRun(ctx, id)
			if err != nil {
				return fmt.Errorf("zeroth attach: %w", err)
			}
			if !runStatusTerminal(run.Status) {
				if _, err := client.foreground(ctx, id); err != nil && !isConflict(err) {
					return fmt.Errorf("zeroth attach: %w", err)
				}
			}

			steerErrs := cmd.ErrOrStderr()
			go scanLines(ctx, cmd.InOrStdin(), func(line string) error {
				if _, err := client.steer(ctx, id, line); err != nil {
					fmt.Fprintf(steerErrs, "zeroth steer: %v\n", err)
				}
				return nil
			})

			err = followRunEvents(ctx, client.base, id, last, "", func(ev gen.RunEvent) error {
				_, werr := fmt.Fprintln(cmd.OutOrStdout(), formatEvent(ev))
				return werr
			})
			if ctx.Err() != nil {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, bgErr := client.background(bgCtx, id); bgErr != nil && !isConflict(bgErr) {
					fmt.Fprintf(steerErrs, "zeroth attach detach: %v\n", bgErr)
				}
				return nil
			}
			return err
		},
	}
	cmd.Flags().IntVar(&last, "last", defaultAttachLast, "how many recent events to replay")
	return cmd
}

func runStatusTerminal(s gen.RunStatus) bool {
	switch s {
	case gen.RunStatusCompleted, gen.RunStatusFailed, gen.RunStatusStopped, gen.RunStatusCancelled:
		return true
	default:
		return false
	}
}
