package sqlite

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

const sessionCols = `id, agent_id, plan_id, status, prompt, tracker_ref, workspace_json, autonomy_tier, created_at_unix_nano, updated_at_unix_nano, finished_at_unix_nano`

func (s *Store) CreateSession(ctx context.Context, sess store.Session) error {
	if err := s.guard(); err != nil {
		return err
	}
	if sess.ID.IsZero() || sess.AgentID.IsZero() || sess.Status == "" {
		return wrap("create session", store.ErrInvalid)
	}
	ws, err := marshalWorkspace(sess.Workspace)
	if err != nil {
		return wrap("create session", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID.String(), sess.AgentID.String(), sess.PlanID.String(), sess.Status, sess.Prompt, sess.TrackerRef,
		ws, sess.AutonomyTier, nano(sess.CreatedAt), nano(sess.UpdatedAt), toNullNano(sess.FinishedAt),
	)
	if err != nil {
		return wrap("create session", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, id store.SessionID) (store.Session, error) {
	if err := s.guard(); err != nil {
		return store.Session{}, err
	}
	if id.IsZero() {
		return store.Session{}, wrap("get session", store.ErrInvalid)
	}
	return scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id.String()), "get session")
}

func (s *Store) UpdateSession(ctx context.Context, sess store.Session) error {
	if err := s.guard(); err != nil {
		return err
	}
	if sess.ID.IsZero() || sess.AgentID.IsZero() || sess.Status == "" {
		return wrap("update session", store.ErrInvalid)
	}
	ws, err := marshalWorkspace(sess.Workspace)
	if err != nil {
		return wrap("update session", err)
	}
	planID := sess.PlanID.String()
	// A zero PlanID is a status-only sync. Keep the attached plan if one
	// exists so a concurrent draft cannot lose the row.
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET agent_id = ?,
			plan_id = CASE WHEN ? = '' THEN plan_id ELSE ? END,
			status = ?, prompt = ?, tracker_ref = ?,
			workspace_json = ?, autonomy_tier = ?, updated_at_unix_nano = ?, finished_at_unix_nano = ?
		WHERE id = ?`,
		sess.AgentID.String(), planID, planID, sess.Status, sess.Prompt, sess.TrackerRef, ws,
		sess.AutonomyTier, nano(sess.UpdatedAt), toNullNano(sess.FinishedAt), sess.ID.String(),
	)
	if err != nil {
		return wrap("update session", err)
	}
	return affectedOne(res, "update session")
}

func (s *Store) ListSessions(ctx context.Context, q store.SessionQuery) (store.Page[store.Session], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.Session]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	var conds []string
	var args []any
	if q.Status != "" {
		conds = append(conds, `status = ?`)
		args = append(args, q.Status)
	}
	if !q.AgentID.IsZero() {
		conds = append(conds, `agent_id = ?`)
		args = append(args, q.AgentID.String())
	}
	cw, cargs, err := cursorWhere("created_at_unix_nano", listNewestFirst, q.Cursor)
	if err != nil {
		return store.Page[store.Session]{}, wrap("list sessions", err)
	}
	if cw != "" {
		conds = append(conds, cw)
		args = append(args, cargs...)
	}
	query := `SELECT ` + sessionCols + ` FROM sessions`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY created_at_unix_nano DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.Session]{}, wrap("list sessions", err)
	}
	defer rows.Close()
	items, err := scanSessions(rows)
	if err != nil {
		return store.Page[store.Session]{}, err
	}
	return pageOf(items, limit, func(x store.Session) time.Time { return x.CreatedAt }, func(x store.Session) string { return x.ID.String() }), nil
}

func scanSession(row *sql.Row, op string) (store.Session, error) {
	var sess store.Session
	var id, agent, plan, ws string
	var created, updated int64
	var finished sql.NullInt64
	err := row.Scan(&id, &agent, &plan, &sess.Status, &sess.Prompt, &sess.TrackerRef, &ws, &sess.AutonomyTier, &created, &updated, &finished)
	if err != nil {
		return store.Session{}, wrap(op, err)
	}
	return finishSession(sess, id, agent, plan, ws, created, updated, finished, op)
}

func scanSessions(rows *sql.Rows) ([]store.Session, error) {
	var out []store.Session
	for rows.Next() {
		var sess store.Session
		var id, agent, plan, ws string
		var created, updated int64
		var finished sql.NullInt64
		if err := rows.Scan(&id, &agent, &plan, &sess.Status, &sess.Prompt, &sess.TrackerRef, &ws, &sess.AutonomyTier, &created, &updated, &finished); err != nil {
			return nil, wrap("list sessions", err)
		}
		sess, err := finishSession(sess, id, agent, plan, ws, created, updated, finished, "list sessions")
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list sessions", err)
	}
	if out == nil {
		out = []store.Session{}
	}
	return out, nil
}

func finishSession(sess store.Session, id, agent, plan, ws string, created, updated int64, finished sql.NullInt64, op string) (store.Session, error) {
	sid, err := store.ParseSessionID(id)
	if err != nil {
		return store.Session{}, wrap(op, err)
	}
	aid, err := store.ParseAgentID(agent)
	if err != nil {
		return store.Session{}, wrap(op, err)
	}
	sess.ID = sid
	sess.AgentID = aid
	if plan != "" {
		pid, err := store.ParsePlanID(plan)
		if err != nil {
			return store.Session{}, wrap(op, err)
		}
		sess.PlanID = pid
	}
	sess.Workspace, err = unmarshalWorkspace(ws)
	if err != nil {
		return store.Session{}, wrap(op, err)
	}
	sess.CreatedAt = fromNano(created)
	sess.UpdatedAt = fromNano(updated)
	sess.FinishedAt = nullNano(finished)
	return sess, nil
}

func (s *Store) AppendEvent(ctx context.Context, sessionID store.SessionID, ev store.Event) (store.Event, error) {
	out, err := s.AppendEvents(ctx, sessionID, []store.Event{ev})
	if err != nil {
		return store.Event{}, err
	}
	return out[0], nil
}

func (s *Store) AppendEvents(ctx context.Context, sessionID store.SessionID, events []store.Event) ([]store.Event, error) {
	if err := s.guard(); err != nil {
		return nil, err
	}
	if sessionID.IsZero() {
		return nil, wrap("append events", store.ErrInvalid)
	}
	if len(events) == 0 {
		return nil, wrap("append events", store.ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, wrap("append events begin", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO events (session_id, type, plan_id, message, payload, created_at_unix_nano)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, wrap("append events prepare", err)
	}
	defer stmt.Close()

	out := make([]store.Event, 0, len(events))
	sid := sessionID.String()
	for _, ev := range events {
		if ev.Type == "" {
			return nil, wrap("append events", store.ErrInvalid)
		}
		created := ev.CreatedAt
		if created.IsZero() {
			created = time.Now().UTC()
		} else {
			created = created.UTC()
		}
		res, err := stmt.ExecContext(ctx, sid, ev.Type, ev.PlanID.String(), ev.Message, ev.Payload, created.UnixNano())
		if err != nil {
			return nil, wrap("append events", err)
		}
		seq, err := res.LastInsertId()
		if err != nil {
			return nil, wrap("append events seq", err)
		}
		eid, err := store.ParseEventID(strconv.FormatInt(seq, 10))
		if err != nil {
			return nil, wrap("append events", err)
		}
		out = append(out, store.Event{
			Seq:       seq,
			ID:        eid,
			SessionID: sessionID,
			Type:      ev.Type,
			PlanID:    ev.PlanID,
			Message:   ev.Message,
			Payload:   ev.Payload,
			CreatedAt: created,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, wrap("append events commit", err)
	}
	return out, nil
}

func (s *Store) ReplayLast(ctx context.Context, sessionID store.SessionID, n int) ([]store.Event, error) {
	if err := s.guard(); err != nil {
		return nil, err
	}
	if sessionID.IsZero() || n < 1 {
		return nil, wrap("replay last", store.ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, session_id, type, plan_id, message, payload, created_at_unix_nano
		FROM (
			SELECT seq, session_id, type, plan_id, message, payload, created_at_unix_nano
			FROM events
			WHERE session_id = ?
			ORDER BY seq DESC
			LIMIT ?
		) t
		ORDER BY seq ASC`, sessionID.String(), n)
	if err != nil {
		return nil, wrap("replay last", err)
	}
	defer rows.Close()
	return scanEvents(rows, "replay last")
}

func (s *Store) EventsAfter(ctx context.Context, sessionID store.SessionID, afterSeq int64) ([]store.Event, error) {
	if err := s.guard(); err != nil {
		return nil, err
	}
	if sessionID.IsZero() {
		return nil, wrap("events after", store.ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, session_id, type, plan_id, message, payload, created_at_unix_nano
		FROM events
		WHERE session_id = ? AND seq > ?
		ORDER BY seq ASC`, sessionID.String(), afterSeq)
	if err != nil {
		return nil, wrap("events after", err)
	}
	defer rows.Close()
	return scanEvents(rows, "events after")
}

func scanEvents(rows *sql.Rows, op string) ([]store.Event, error) {
	var out []store.Event
	for rows.Next() {
		var ev store.Event
		var rawSess, plan string
		var nanoTs int64
		if err := rows.Scan(&ev.Seq, &rawSess, &ev.Type, &plan, &ev.Message, &ev.Payload, &nanoTs); err != nil {
			return nil, wrap(op, err)
		}
		sid, err := store.ParseSessionID(rawSess)
		if err != nil {
			return nil, wrap(op, err)
		}
		eid, err := store.ParseEventID(strconv.FormatInt(ev.Seq, 10))
		if err != nil {
			return nil, wrap(op, err)
		}
		ev.ID = eid
		ev.SessionID = sid
		if plan != "" {
			pid, err := store.ParsePlanID(plan)
			if err != nil {
				return nil, wrap(op, err)
			}
			ev.PlanID = pid
		}
		ev.CreatedAt = fromNano(nanoTs)
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(op, err)
	}
	if out == nil {
		out = []store.Event{}
	}
	return out, nil
}

func (s *Store) CreateCheckpoint(ctx context.Context, c store.Checkpoint) error {
	if err := s.guard(); err != nil {
		return err
	}
	if c.ID.IsZero() || c.SessionID.IsZero() {
		return wrap("create checkpoint", store.ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO checkpoints (id, session_id, label, location, created_at_unix_nano)
		VALUES (?, ?, ?, ?, ?)`,
		c.ID.String(), c.SessionID.String(), c.Label, c.Location, nano(c.CreatedAt),
	)
	if err != nil {
		return wrap("create checkpoint", err)
	}
	return nil
}

func (s *Store) GetCheckpoint(ctx context.Context, id store.CheckpointID) (store.Checkpoint, error) {
	if err := s.guard(); err != nil {
		return store.Checkpoint{}, err
	}
	if id.IsZero() {
		return store.Checkpoint{}, wrap("get checkpoint", store.ErrInvalid)
	}
	var rawID, sess, label, loc string
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, label, location, created_at_unix_nano FROM checkpoints WHERE id = ?`, id.String(),
	).Scan(&rawID, &sess, &label, &loc, &created)
	if err != nil {
		return store.Checkpoint{}, wrap("get checkpoint", err)
	}
	return parseCheckpoint(rawID, sess, label, loc, created)
}

func (s *Store) ListCheckpoints(ctx context.Context, q store.CheckpointQuery) (store.Page[store.Checkpoint], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.Checkpoint]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	var conds []string
	var args []any
	if !q.SessionID.IsZero() {
		conds = append(conds, `session_id = ?`)
		args = append(args, q.SessionID.String())
	}
	cw, cargs, err := cursorWhere("created_at_unix_nano", listNewestFirst, q.Cursor)
	if err != nil {
		return store.Page[store.Checkpoint]{}, wrap("list checkpoints", err)
	}
	if cw != "" {
		conds = append(conds, cw)
		args = append(args, cargs...)
	}
	query := `SELECT id, session_id, label, location, created_at_unix_nano FROM checkpoints`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY created_at_unix_nano DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.Checkpoint]{}, wrap("list checkpoints", err)
	}
	defer rows.Close()
	var items []store.Checkpoint
	for rows.Next() {
		var rawID, sess, label, loc string
		var created int64
		if err := rows.Scan(&rawID, &sess, &label, &loc, &created); err != nil {
			return store.Page[store.Checkpoint]{}, wrap("list checkpoints", err)
		}
		c, err := parseCheckpoint(rawID, sess, label, loc, created)
		if err != nil {
			return store.Page[store.Checkpoint]{}, err
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return store.Page[store.Checkpoint]{}, wrap("list checkpoints", err)
	}
	return pageOf(items, limit, func(c store.Checkpoint) time.Time { return c.CreatedAt }, func(c store.Checkpoint) string { return c.ID.String() }), nil
}

func parseCheckpoint(rawID, sess, label, loc string, created int64) (store.Checkpoint, error) {
	id, err := store.ParseCheckpointID(rawID)
	if err != nil {
		return store.Checkpoint{}, wrap("checkpoint", err)
	}
	sid, err := store.ParseSessionID(sess)
	if err != nil {
		return store.Checkpoint{}, wrap("checkpoint", err)
	}
	return store.Checkpoint{
		ID:        id,
		SessionID: sid,
		Label:     label,
		Location:  loc,
		CreatedAt: fromNano(created),
	}, nil
}
