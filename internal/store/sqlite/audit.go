package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

const auditCols = `id, action, target, plan_hash, precondition, postcondition, lease_id, approver, agent_pubkey, prev_hash, hash, signature, agent_id, session_id, resource_type, resource_id, actor, created_at_unix_nano`

func (s *Store) AppendAudit(ctx context.Context, r store.AuditRecord) (store.AuditRecord, error) {
	if err := s.guard(); err != nil {
		return store.AuditRecord{}, err
	}
	if r.ID.IsZero() || r.Action == "" || r.ResourceType == "" || r.ResourceID == "" || r.Signature == "" || r.Hash == "" || r.AgentPubKey == "" {
		return store.AuditRecord{}, wrap("append audit", store.ErrInvalid)
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	} else {
		r.CreatedAt = r.CreatedAt.UTC()
	}
	if r.Target == "" {
		r.Target = r.ResourceID
	}
	if r.Actor == "" {
		r.Actor = r.Approver
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_records (`+auditCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID.String(), r.Action, r.Target, r.PlanHash, r.Precondition, r.Postcondition,
		r.LeaseID.String(), r.Approver, r.AgentPubKey, r.PrevHash, r.Hash, r.Signature,
		r.AgentID.String(), r.SessionID.String(), r.ResourceType, r.ResourceID, r.Actor,
		r.CreatedAt.UnixNano(),
	)
	if err != nil {
		return store.AuditRecord{}, wrap("append audit", err)
	}
	return r, nil
}

func (s *Store) GetAudit(ctx context.Context, id store.AuditID) (store.AuditRecord, error) {
	if err := s.guard(); err != nil {
		return store.AuditRecord{}, err
	}
	if id.IsZero() {
		return store.AuditRecord{}, wrap("get audit", store.ErrInvalid)
	}
	r, err := scanAudit(s.db.QueryRowContext(ctx, `SELECT `+auditCols+` FROM audit_records WHERE id = ?`, id.String()))
	if err != nil {
		return store.AuditRecord{}, wrap("get audit", err)
	}
	return r, nil
}

func (s *Store) ListAudit(ctx context.Context, q store.AuditQuery) (store.Page[store.AuditRecord], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.AuditRecord]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	var conds []string
	var args []any
	if q.ResourceType != "" {
		conds = append(conds, `resource_type = ?`)
		args = append(args, q.ResourceType)
	}
	if q.ResourceID != "" {
		conds = append(conds, `resource_id = ?`)
		args = append(args, q.ResourceID)
	}
	if !q.SessionID.IsZero() {
		conds = append(conds, `session_id = ?`)
		args = append(args, q.SessionID.String())
	}
	cw, cargs, err := cursorWhere("created_at_unix_nano", listNewestFirst, q.Cursor)
	if err != nil {
		return store.Page[store.AuditRecord]{}, wrap("list audit", err)
	}
	if cw != "" {
		conds = append(conds, cw)
		args = append(args, cargs...)
	}
	query := `SELECT ` + auditCols + ` FROM audit_records`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY created_at_unix_nano DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.AuditRecord]{}, wrap("list audit", err)
	}
	defer rows.Close()
	items, err := scanAudits(rows, "list audit")
	if err != nil {
		return store.Page[store.AuditRecord]{}, err
	}
	return pageOf(items, limit, func(r store.AuditRecord) time.Time { return r.CreatedAt }, func(r store.AuditRecord) string { return r.ID.String() }), nil
}

func (s *Store) AuditChain(ctx context.Context) ([]store.AuditRecord, error) {
	if err := s.guard(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+auditCols+` FROM audit_records ORDER BY created_at_unix_nano ASC, id ASC`)
	if err != nil {
		return nil, wrap("audit chain", err)
	}
	defer rows.Close()
	items, err := scanAudits(rows, "audit chain")
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []store.AuditRecord{}
	}
	return items, nil
}

func (s *Store) AppendAgentKey(ctx context.Context, k store.AgentKey) error {
	if err := s.guard(); err != nil {
		return err
	}
	if k.AgentID.IsZero() || k.PubKey == "" {
		return wrap("append agent key", store.ErrInvalid)
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	} else {
		k.CreatedAt = k.CreatedAt.UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_keys (pubkey, agent_id, created_at_unix_nano)
		VALUES (?, ?, ?)`,
		k.PubKey, k.AgentID.String(), k.CreatedAt.UnixNano(),
	)
	if err != nil {
		return wrap("append agent key", err)
	}
	return nil
}

func (s *Store) ListAgentKeys(ctx context.Context, agentID store.AgentID) ([]store.AgentKey, error) {
	if err := s.guard(); err != nil {
		return nil, err
	}
	query := `SELECT pubkey, agent_id, created_at_unix_nano FROM agent_keys`
	var args []any
	if !agentID.IsZero() {
		query += ` WHERE agent_id = ?`
		args = append(args, agentID.String())
	}
	query += ` ORDER BY created_at_unix_nano ASC, pubkey ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrap("list agent keys", err)
	}
	defer rows.Close()
	var out []store.AgentKey
	for rows.Next() {
		var pub, agent string
		var created int64
		if err := rows.Scan(&pub, &agent, &created); err != nil {
			return nil, wrap("list agent keys", err)
		}
		aid, err := store.ParseAgentID(agent)
		if err != nil {
			return nil, wrap("list agent keys", err)
		}
		out = append(out, store.AgentKey{AgentID: aid, PubKey: pub, CreatedAt: fromNano(created)})
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list agent keys", err)
	}
	if out == nil {
		out = []store.AgentKey{}
	}
	return out, nil
}

func scanAudit(row *sql.Row) (store.AuditRecord, error) {
	var r store.AuditRecord
	var rawID, lease, agent, session string
	var created int64
	err := row.Scan(
		&rawID, &r.Action, &r.Target, &r.PlanHash, &r.Precondition, &r.Postcondition,
		&lease, &r.Approver, &r.AgentPubKey, &r.PrevHash, &r.Hash, &r.Signature,
		&agent, &session, &r.ResourceType, &r.ResourceID, &r.Actor, &created,
	)
	if err != nil {
		return store.AuditRecord{}, err
	}
	return finishAudit(r, rawID, lease, agent, session, created)
}

func scanAudits(rows *sql.Rows, op string) ([]store.AuditRecord, error) {
	var items []store.AuditRecord
	for rows.Next() {
		var r store.AuditRecord
		var rawID, lease, agent, session string
		var created int64
		if err := rows.Scan(
			&rawID, &r.Action, &r.Target, &r.PlanHash, &r.Precondition, &r.Postcondition,
			&lease, &r.Approver, &r.AgentPubKey, &r.PrevHash, &r.Hash, &r.Signature,
			&agent, &session, &r.ResourceType, &r.ResourceID, &r.Actor, &created,
		); err != nil {
			return nil, wrap(op, err)
		}
		r, err := finishAudit(r, rawID, lease, agent, session, created)
		if err != nil {
			return nil, wrap(op, err)
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(op, err)
	}
	if items == nil {
		items = []store.AuditRecord{}
	}
	return items, nil
}

func finishAudit(r store.AuditRecord, rawID, lease, agent, session string, created int64) (store.AuditRecord, error) {
	id, err := store.ParseAuditID(rawID)
	if err != nil {
		return store.AuditRecord{}, err
	}
	r.ID = id
	if lease != "" {
		lid, err := store.ParseLeaseID(lease)
		if err != nil {
			return store.AuditRecord{}, err
		}
		r.LeaseID = lid
	}
	if agent != "" {
		aid, err := store.ParseAgentID(agent)
		if err != nil {
			return store.AuditRecord{}, err
		}
		r.AgentID = aid
	}
	if session != "" {
		sid, err := store.ParseSessionID(session)
		if err != nil {
			return store.AuditRecord{}, err
		}
		r.SessionID = sid
	}
	r.CreatedAt = fromNano(created)
	return r, nil
}
