package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/store"
)

const memorySelect = `id, kind, ref_id, content, fact_key, author, author_kind, source, action, deleted, version, created_at_unix_nano, updated_at_unix_nano, history_json`

func (s *Store) CreateMemory(ctx context.Context, m store.MemoryEntry) error {
	if err := s.guard(); err != nil {
		return err
	}
	if m.ID.IsZero() || m.Kind == "" || m.Content == "" {
		return wrap("create memory", store.ErrInvalid)
	}
	hist, err := marshalHistory(m.History)
	if err != nil {
		return wrap("create memory", err)
	}
	updated := m.UpdatedAt
	if updated.IsZero() {
		updated = m.CreatedAt
	}
	deleted := 0
	if m.Deleted {
		deleted = 1
	}
	version := m.Version
	if version == 0 {
		version = 1
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO memory_entries (id, kind, ref_id, content, fact_key, author, author_kind, source, action, deleted, version, created_at_unix_nano, updated_at_unix_nano, history_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID.String(), m.Kind, m.RefID, m.Content, m.Key, m.Author, m.AuthorKind, m.Source, m.Action,
		deleted, version, nano(m.CreatedAt), unixNano(updated), hist,
	)
	if err != nil {
		return wrap("create memory", err)
	}
	return nil
}

func (s *Store) GetMemory(ctx context.Context, id store.MemoryID) (store.MemoryEntry, error) {
	if err := s.guard(); err != nil {
		return store.MemoryEntry{}, err
	}
	if id.IsZero() {
		return store.MemoryEntry{}, wrap("get memory", store.ErrInvalid)
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+memorySelect+` FROM memory_entries WHERE id = ?`, id.String())
	m, err := scanMemory(row)
	if err != nil {
		return store.MemoryEntry{}, wrap("get memory", err)
	}
	return m, nil
}

func (s *Store) UpdateMemory(ctx context.Context, m store.MemoryEntry) error {
	if err := s.guard(); err != nil {
		return err
	}
	if m.ID.IsZero() || m.Kind == "" || m.Content == "" {
		return wrap("update memory", store.ErrInvalid)
	}
	hist, err := marshalHistory(m.History)
	if err != nil {
		return wrap("update memory", err)
	}
	deleted := 0
	if m.Deleted {
		deleted = 1
	}
	version := m.Version
	if version == 0 {
		version = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE memory_entries SET kind = ?, ref_id = ?, content = ?, fact_key = ?, author = ?, author_kind = ?,
			source = ?, action = ?, deleted = ?, version = ?, updated_at_unix_nano = ?, history_json = ?
		WHERE id = ?`,
		m.Kind, m.RefID, m.Content, m.Key, m.Author, m.AuthorKind, m.Source, m.Action,
		deleted, version, unixNano(m.UpdatedAt), hist, m.ID.String(),
	)
	if err != nil {
		return wrap("update memory", err)
	}
	return affectedOne(res, "update memory")
}

func (s *Store) ListMemory(ctx context.Context, q store.MemoryQuery) (store.Page[store.MemoryEntry], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.MemoryEntry]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	var conds []string
	var args []any
	if q.Kind != "" {
		conds = append(conds, `kind = ?`)
		args = append(args, q.Kind)
	}
	if q.RefID != "" {
		conds = append(conds, `ref_id = ?`)
		args = append(args, q.RefID)
	}
	if q.Key != "" {
		conds = append(conds, `fact_key = ?`)
		args = append(args, q.Key)
	}
	cw, cargs, err := cursorWhere("created_at_unix_nano", listNewestFirst, q.Cursor)
	if err != nil {
		return store.Page[store.MemoryEntry]{}, wrap("list memory", err)
	}
	if cw != "" {
		conds = append(conds, cw)
		args = append(args, cargs...)
	}
	query := `SELECT ` + memorySelect + ` FROM memory_entries`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY created_at_unix_nano DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.MemoryEntry]{}, wrap("list memory", err)
	}
	defer rows.Close()
	var items []store.MemoryEntry
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return store.Page[store.MemoryEntry]{}, wrap("list memory", err)
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return store.Page[store.MemoryEntry]{}, wrap("list memory", err)
	}
	return pageOf(items, limit, func(m store.MemoryEntry) time.Time { return m.CreatedAt }, func(m store.MemoryEntry) string { return m.ID.String() }), nil
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemory(row memoryScanner) (store.MemoryEntry, error) {
	var rawID, hist string
	var created, updated int64
	var deleted, version int
	var m store.MemoryEntry
	err := row.Scan(&rawID, &m.Kind, &m.RefID, &m.Content, &m.Key, &m.Author, &m.AuthorKind, &m.Source, &m.Action,
		&deleted, &version, &created, &updated, &hist)
	if err != nil {
		return store.MemoryEntry{}, err
	}
	mid, err := store.ParseMemoryID(rawID)
	if err != nil {
		return store.MemoryEntry{}, err
	}
	m.ID = mid
	m.Deleted = deleted != 0
	m.Version = version
	m.CreatedAt = fromNano(created)
	m.UpdatedAt = fromNano(updated)
	m.History, err = unmarshalHistory(hist)
	if err != nil {
		return store.MemoryEntry{}, err
	}
	return m, nil
}

func (s *Store) CreateMemoryProposal(ctx context.Context, p store.MemoryProposal) error {
	if err := s.guard(); err != nil {
		return err
	}
	if p.ID.IsZero() || p.Kind == "" || p.Content == "" || p.Status == "" {
		return wrap("create memory proposal", store.ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO memory_proposals (id, kind, ref_id, session_id, content, status, memory_id, created_at_unix_nano, reviewed_at_unix_nano, fact_key, author, author_kind, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID.String(), p.Kind, p.RefID, p.SessionID.String(), p.Content, p.Status, p.MemoryID.String(),
		nano(p.CreatedAt), toNullNano(p.ReviewedAt), p.Key, p.Author, p.AuthorKind, p.Source,
	)
	if err != nil {
		return wrap("create memory proposal", err)
	}
	return nil
}

func (s *Store) GetMemoryProposal(ctx context.Context, id store.MemoryProposalID) (store.MemoryProposal, error) {
	if err := s.guard(); err != nil {
		return store.MemoryProposal{}, err
	}
	if id.IsZero() {
		return store.MemoryProposal{}, wrap("get memory proposal", store.ErrInvalid)
	}
	var rawID, sess, mem string
	var created int64
	var reviewed sql.NullInt64
	var p store.MemoryProposal
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, ref_id, session_id, content, status, memory_id, created_at_unix_nano, reviewed_at_unix_nano, fact_key, author, author_kind, source
		FROM memory_proposals WHERE id = ?`, id.String(),
	).Scan(&rawID, &p.Kind, &p.RefID, &sess, &p.Content, &p.Status, &mem, &created, &reviewed, &p.Key, &p.Author, &p.AuthorKind, &p.Source)
	if err != nil {
		return store.MemoryProposal{}, wrap("get memory proposal", err)
	}
	return finishProposal(p, rawID, sess, mem, created, reviewed, "get memory proposal")
}

func (s *Store) UpdateMemoryProposal(ctx context.Context, p store.MemoryProposal) error {
	if err := s.guard(); err != nil {
		return err
	}
	if p.ID.IsZero() || p.Kind == "" || p.Content == "" || p.Status == "" {
		return wrap("update memory proposal", store.ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE memory_proposals SET kind = ?, ref_id = ?, session_id = ?, content = ?, status = ?,
			memory_id = ?, reviewed_at_unix_nano = ?, fact_key = ?, author = ?, author_kind = ?, source = ?
		WHERE id = ?`,
		p.Kind, p.RefID, p.SessionID.String(), p.Content, p.Status, p.MemoryID.String(),
		toNullNano(p.ReviewedAt), p.Key, p.Author, p.AuthorKind, p.Source, p.ID.String(),
	)
	if err != nil {
		return wrap("update memory proposal", err)
	}
	return affectedOne(res, "update memory proposal")
}

func (s *Store) ListMemoryProposals(ctx context.Context, q store.MemoryProposalQuery) (store.Page[store.MemoryProposal], error) {
	if err := s.guard(); err != nil {
		return store.Page[store.MemoryProposal]{}, err
	}
	limit := store.ClampLimit(q.Limit)
	var conds []string
	var args []any
	if q.Status != "" {
		conds = append(conds, `status = ?`)
		args = append(args, q.Status)
	}
	cw, cargs, err := cursorWhere("created_at_unix_nano", listNewestFirst, q.Cursor)
	if err != nil {
		return store.Page[store.MemoryProposal]{}, wrap("list memory proposals", err)
	}
	if cw != "" {
		conds = append(conds, cw)
		args = append(args, cargs...)
	}
	query := `SELECT id, kind, ref_id, session_id, content, status, memory_id, created_at_unix_nano, reviewed_at_unix_nano, fact_key, author, author_kind, source FROM memory_proposals`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY created_at_unix_nano DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.Page[store.MemoryProposal]{}, wrap("list memory proposals", err)
	}
	defer rows.Close()
	var items []store.MemoryProposal
	for rows.Next() {
		var p store.MemoryProposal
		var rawID, sess, mem string
		var created int64
		var reviewed sql.NullInt64
		if err := rows.Scan(&rawID, &p.Kind, &p.RefID, &sess, &p.Content, &p.Status, &mem, &created, &reviewed, &p.Key, &p.Author, &p.AuthorKind, &p.Source); err != nil {
			return store.Page[store.MemoryProposal]{}, wrap("list memory proposals", err)
		}
		p, err := finishProposal(p, rawID, sess, mem, created, reviewed, "list memory proposals")
		if err != nil {
			return store.Page[store.MemoryProposal]{}, err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return store.Page[store.MemoryProposal]{}, wrap("list memory proposals", err)
	}
	return pageOf(items, limit, func(p store.MemoryProposal) time.Time { return p.CreatedAt }, func(p store.MemoryProposal) string { return p.ID.String() }), nil
}

func finishProposal(p store.MemoryProposal, rawID, sess, mem string, created int64, reviewed sql.NullInt64, op string) (store.MemoryProposal, error) {
	id, err := store.ParseMemoryProposalID(rawID)
	if err != nil {
		return store.MemoryProposal{}, wrap(op, err)
	}
	p.ID = id
	if sess != "" {
		sid, err := store.ParseSessionID(sess)
		if err != nil {
			return store.MemoryProposal{}, wrap(op, err)
		}
		p.SessionID = sid
	}
	if mem != "" {
		mid, err := store.ParseMemoryID(mem)
		if err != nil {
			return store.MemoryProposal{}, wrap(op, err)
		}
		p.MemoryID = mid
	}
	p.CreatedAt = fromNano(created)
	p.ReviewedAt = nullNano(reviewed)
	return p, nil
}
