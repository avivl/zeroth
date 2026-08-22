package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

const agentCols = `id, name, harness, status, model, tools_json, autonomy_tier, reviewer_model, reviewer_model_2, reviewer_dual, block_on_fail, created_at_unix_nano, updated_at_unix_nano`

func (s *Store) CreateAgent(ctx context.Context, a store.Agent) error {
	if err := s.guard(); err != nil {
		return err
	}
	if a.ID.IsZero() || a.Name == "" || a.Harness == "" || a.Status == "" {
		return wrap("create agent", store.ErrInvalid)
	}
	tools, err := marshalJSON(a.Tools)
	if err != nil {
		return wrap("create agent", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agents (`+agentCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID.String(), a.Name, a.Harness, a.Status, a.Model, tools, a.AutonomyTier,
		a.ReviewerModel, a.ReviewerModel2, boolInt(a.ReviewerDual), boolInt(a.BlockOnFail),
		nano(a.CreatedAt), nano(a.UpdatedAt),
	)
	if err != nil {
		return wrap("create agent", err)
	}
	return nil
}

func (s *Store) GetAgent(ctx context.Context, id store.AgentID) (store.Agent, error) {
	if err := s.guard(); err != nil {
		return store.Agent{}, err
	}
	if id.IsZero() {
		return store.Agent{}, wrap("get agent", store.ErrInvalid)
	}
	return scanAgent(s.db.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, id.String()))
}

func (s *Store) UpdateAgent(ctx context.Context, a store.Agent) error {
	if err := s.guard(); err != nil {
		return err
	}
	if a.ID.IsZero() || a.Name == "" || a.Harness == "" || a.Status == "" {
		return wrap("update agent", store.ErrInvalid)
	}
	tools, err := marshalJSON(a.Tools)
	if err != nil {
		return wrap("update agent", err)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents SET name = ?, harness = ?, status = ?, model = ?, tools_json = ?,
			autonomy_tier = ?, reviewer_model = ?, reviewer_model_2 = ?, reviewer_dual = ?,
			block_on_fail = ?, updated_at_unix_nano = ?
		WHERE id = ?`,
		a.Name, a.Harness, a.Status, a.Model, tools, a.AutonomyTier,
		a.ReviewerModel, a.ReviewerModel2, boolInt(a.ReviewerDual), boolInt(a.BlockOnFail),
		nano(a.UpdatedAt), a.ID.String(),
	)
	if err != nil {
		return wrap("update agent", err)
	}
	return affectedOne(res, "update agent")
}

func (s *Store) ListAgents(ctx context.Context, q store.PageQuery) (store.Page[store.Agent], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.Agent]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	where, args, err := cursorWhere("created_at_unix_nano", listNewestFirst, q.Cursor)
	if err != nil {
		return store.Page[store.Agent]{}, wrap("list agents", err)
	}
	query := `SELECT ` + agentCols + ` FROM agents`
	if where != "" {
		query += ` WHERE ` + where
	}
	query += ` ORDER BY created_at_unix_nano DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.Agent]{}, wrap("list agents", err)
	}
	defer rows.Close()
	items, err := scanAgents(rows)
	if err != nil {
		return store.Page[store.Agent]{}, err
	}
	return pageOf(items, limit, func(a store.Agent) time.Time { return a.CreatedAt }, func(a store.Agent) string { return a.ID.String() }), nil
}

func scanAgent(row *sql.Row) (store.Agent, error) {
	var a store.Agent
	var id, tools string
	var dual, block int
	var created, updated int64
	err := row.Scan(&id, &a.Name, &a.Harness, &a.Status, &a.Model, &tools, &a.AutonomyTier,
		&a.ReviewerModel, &a.ReviewerModel2, &dual, &block, &created, &updated)
	if err != nil {
		return store.Agent{}, wrap("get agent", err)
	}
	a.ReviewerDual = dual != 0
	a.BlockOnFail = block != 0
	return finishAgent(a, id, tools, created, updated, "get agent")
}

func scanAgents(rows *sql.Rows) ([]store.Agent, error) {
	var out []store.Agent
	for rows.Next() {
		var a store.Agent
		var id, tools string
		var dual, block int
		var created, updated int64
		if err := rows.Scan(&id, &a.Name, &a.Harness, &a.Status, &a.Model, &tools, &a.AutonomyTier,
			&a.ReviewerModel, &a.ReviewerModel2, &dual, &block, &created, &updated); err != nil {
			return nil, wrap("list agents", err)
		}
		a.ReviewerDual = dual != 0
		a.BlockOnFail = block != 0
		a, err := finishAgent(a, id, tools, created, updated, "list agents")
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list agents", err)
	}
	if out == nil {
		out = []store.Agent{}
	}
	return out, nil
}

func finishAgent(a store.Agent, id, tools string, created, updated int64, op string) (store.Agent, error) {
	aid, err := store.ParseAgentID(id)
	if err != nil {
		return store.Agent{}, wrap(op, err)
	}
	a.ID = aid
	a.Tools, err = unmarshalSlice[string](tools)
	if err != nil {
		return store.Agent{}, wrap(op, err)
	}
	a.CreatedAt = fromNano(created)
	a.UpdatedAt = fromNano(updated)
	return a, nil
}

func (s *Store) CrossExamStats(ctx context.Context, agentID store.AgentID) (store.CrossExamStats, error) {
	if err := s.guard(); err != nil {
		return store.CrossExamStats{}, err
	}
	if agentID.IsZero() {
		return store.CrossExamStats{}, wrap("cross-exam stats", store.ErrInvalid)
	}
	if _, err := s.GetAgent(ctx, agentID); err != nil {
		return store.CrossExamStats{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.effects_json, p.cross_exam_json
		FROM plans p
		JOIN sessions s ON s.id = p.session_id
		WHERE s.agent_id = ? AND p.cross_exam_json != ''`,
		agentID.String(),
	)
	if err != nil {
		return store.CrossExamStats{}, wrap("cross-exam stats", err)
	}
	defer rows.Close()
	stats := store.CrossExamStats{AgentID: agentID}
	for rows.Next() {
		var effectsJSON, examJSON string
		if err := rows.Scan(&effectsJSON, &examJSON); err != nil {
			return store.CrossExamStats{}, wrap("cross-exam stats", err)
		}
		exam, err := unmarshalCrossExam(examJSON)
		if err != nil {
			return store.CrossExamStats{}, wrap("cross-exam stats", err)
		}
		if exam == nil || exam.Verdict == "" {
			continue
		}
		effects, err := unmarshalEffects(effectsJSON)
		if err != nil {
			return store.CrossExamStats{}, wrap("cross-exam stats", err)
		}
		stats.Examined++
		switch exam.Verdict {
		case "pass":
			stats.Pass++
		case "fail":
			stats.Fail++
		case "pass_with_notes":
			stats.PassWithNotes++
		}
		if strings.TrimSpace(exam.Reasoning) == "" && nontrivialEffects(effects) {
			stats.EmptyNotesNontrivial++
		}
	}
	if err := rows.Err(); err != nil {
		return store.CrossExamStats{}, wrap("cross-exam stats", err)
	}
	return stats, nil
}

func nontrivialEffects(effects []store.PlanEffect) bool {
	if len(effects) > 1 {
		return true
	}
	for _, e := range effects {
		if len(e.Diff) > 80 {
			return true
		}
	}
	return false
}

func (s *Store) CreateLease(ctx context.Context, l store.Lease) error {
	if err := s.guard(); err != nil {
		return err
	}
	if l.ID.IsZero() || l.GrantID.IsZero() || l.ScopeID.IsZero() || l.AgentID.IsZero() || l.ExpiresAt.IsZero() {
		return wrap("create lease", store.ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO leases (id, grant_id, scope_id, agent_id, expires_at_unix_nano, minted_at_unix_nano)
		VALUES (?, ?, ?, ?, ?, ?)`,
		l.ID.String(), l.GrantID.String(), l.ScopeID.String(), l.AgentID.String(),
		l.ExpiresAt.UTC().UnixNano(), nano(l.MintedAt),
	)
	if err != nil {
		return wrap("create lease", err)
	}
	return nil
}

func (s *Store) GetLease(ctx context.Context, id store.LeaseID) (store.Lease, error) {
	if err := s.guard(); err != nil {
		return store.Lease{}, err
	}
	if id.IsZero() {
		return store.Lease{}, wrap("get lease", store.ErrInvalid)
	}
	var rawID, grant, scope, agent string
	var exp, minted int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, grant_id, scope_id, agent_id, expires_at_unix_nano, minted_at_unix_nano
		FROM leases WHERE id = ?`, id.String(),
	).Scan(&rawID, &grant, &scope, &agent, &exp, &minted)
	if err != nil {
		return store.Lease{}, wrap("get lease", err)
	}
	return parseLease(rawID, grant, scope, agent, exp, minted)
}

func (s *Store) UpdateLease(ctx context.Context, l store.Lease) error {
	if err := s.guard(); err != nil {
		return err
	}
	if l.ID.IsZero() || l.GrantID.IsZero() || l.ScopeID.IsZero() || l.AgentID.IsZero() || l.ExpiresAt.IsZero() {
		return wrap("update lease", store.ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE leases SET grant_id = ?, scope_id = ?, agent_id = ?, expires_at_unix_nano = ?
		WHERE id = ?`,
		l.GrantID.String(), l.ScopeID.String(), l.AgentID.String(), l.ExpiresAt.UTC().UnixNano(), l.ID.String(),
	)
	if err != nil {
		return wrap("update lease", err)
	}
	return affectedOne(res, "update lease")
}

func (s *Store) ListLeases(ctx context.Context, q store.LeaseQuery) (store.Page[store.Lease], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.Lease]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	var conds []string
	var args []any
	if !q.AgentID.IsZero() {
		conds = append(conds, `agent_id = ?`)
		args = append(args, q.AgentID.String())
	}
	cw, cargs, err := cursorWhere("minted_at_unix_nano", listNewestFirst, q.Cursor)
	if err != nil {
		return store.Page[store.Lease]{}, wrap("list leases", err)
	}
	if cw != "" {
		conds = append(conds, cw)
		args = append(args, cargs...)
	}
	query := `SELECT id, grant_id, scope_id, agent_id, expires_at_unix_nano, minted_at_unix_nano FROM leases`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY minted_at_unix_nano DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.Lease]{}, wrap("list leases", err)
	}
	defer rows.Close()
	var items []store.Lease
	for rows.Next() {
		var rawID, grant, scope, agent string
		var exp, minted int64
		if err := rows.Scan(&rawID, &grant, &scope, &agent, &exp, &minted); err != nil {
			return store.Page[store.Lease]{}, wrap("list leases", err)
		}
		l, err := parseLease(rawID, grant, scope, agent, exp, minted)
		if err != nil {
			return store.Page[store.Lease]{}, err
		}
		items = append(items, l)
	}
	if err := rows.Err(); err != nil {
		return store.Page[store.Lease]{}, wrap("list leases", err)
	}
	return pageOf(items, limit, func(l store.Lease) time.Time { return l.MintedAt }, func(l store.Lease) string { return l.ID.String() }), nil
}

func parseLease(rawID, grant, scope, agent string, exp, minted int64) (store.Lease, error) {
	id, err := store.ParseLeaseID(rawID)
	if err != nil {
		return store.Lease{}, wrap("lease", err)
	}
	gid, err := store.ParseGrantID(grant)
	if err != nil {
		return store.Lease{}, wrap("lease", err)
	}
	sid, err := store.ParseScopeID(scope)
	if err != nil {
		return store.Lease{}, wrap("lease", err)
	}
	aid, err := store.ParseAgentID(agent)
	if err != nil {
		return store.Lease{}, wrap("lease", err)
	}
	return store.Lease{
		ID:        id,
		GrantID:   gid,
		ScopeID:   sid,
		AgentID:   aid,
		ExpiresAt: fromNano(exp),
		MintedAt:  fromNano(minted),
	}, nil
}
