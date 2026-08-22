package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func (c *cli) newVerifyCmd() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "verify <run-id>",
		Short: "Verify a run's signed audit chain offline (no daemon)",
		Long: `Verify a run's audit hash chain against the local SQLite file.

Does not contact zerothd and does not use the network. An auditor who
does not trust the daemon can still check the record. Tampering names
the record; a missing middle row names where the chain breaks.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(cmd, strings.TrimSpace(dbPath), strings.TrimSpace(args[0]))
		},
	}
	cmd.Flags().StringVar(&dbPath, "db-path", envOr("ZEROTH_DB_PATH", "zeroth.db"), "sqlite database path (ZEROTH_DB_PATH)")
	return cmd
}

func runVerify(cmd *cobra.Command, dbPath, runID string) error {
	if runID == "" {
		return fmt.Errorf("zeroth verify: empty run id")
	}
	sid, err := store.ParseSessionID(runID)
	if err != nil {
		return fmt.Errorf("zeroth verify: %w", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("zeroth verify: database %s not found (is ZEROTH_DB_PATH / --db-path set?)", dbPath)
		}
		return fmt.Errorf("zeroth verify: %w", err)
	}
	st, err := sqlite.New(dbPath)
	if err != nil {
		return fmt.Errorf("zeroth verify: open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	ctx := cmd.Context()
	if _, err := st.GetSession(ctx, sid); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("zeroth verify: run %s not found", runID)
		}
		return fmt.Errorf("zeroth verify: %w", err)
	}
	chain, err := st.AuditChain(ctx)
	if err != nil {
		return fmt.Errorf("zeroth verify: %w", err)
	}
	keys, err := st.ListAgentKeys(ctx, store.AgentID{})
	if err != nil {
		return fmt.Errorf("zeroth verify: %w", err)
	}
	if err := audit.VerifyChain(chain, keys); err != nil {
		return fmt.Errorf("zeroth verify: %w", err)
	}
	var forRun int
	for _, rec := range chain {
		if rec.SessionID == sid {
			forRun++
		}
	}
	if forRun == 0 {
		return fmt.Errorf("zeroth verify: run %s has no audit records", runID)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "ok\t%s\t%d records\n", runID, len(chain))
	return err
}
