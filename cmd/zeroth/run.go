package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/avivl/zeroth/internal/server"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"github.com/spf13/cobra"
)

func (c *cli) newRunCmd() *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "run <task>",
		Short: "Start a headless run against local zerothd",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client := c.client()
			agentID := strings.TrimSpace(agent)
			if agentID == "" {
				id, err := pickAgent(ctx, client)
				if err != nil {
					return err
				}
				agentID = id
			}
			prompt := strings.Join(args, " ")
			run, err := client.createRun(ctx, gen.CreateRunRequest{
				AgentId: gen.AgentID(agentID),
				Prompt:  &prompt,
			})
			if err != nil {
				return err
			}
			if _, err := client.background(ctx, string(run.Id)); err != nil && !isConflict(err) {
				return fmt.Errorf("zeroth run background: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), run.Id)
			return err
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent id (defaults to the daemon's local agent)")
	return cmd
}

func pickAgent(ctx context.Context, client *apiClient) (string, error) {
	list, err := client.listAgents(ctx)
	if err != nil {
		return "", err
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("zeroth run: no agents on the daemon")
	}
	for _, a := range list.Items {
		if string(a.Id) == server.DefaultAgentID {
			return string(a.Id), nil
		}
	}
	return string(list.Items[0].Id), nil
}
