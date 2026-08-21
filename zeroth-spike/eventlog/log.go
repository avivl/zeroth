package eventlog

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/session"
	_ "modernc.org/sqlite"
)

const (
	// TypeCreated is stored when the session row is inserted.
	TypeCreated = "created"
	// TypeStarted is stored when the supervisor starts the agent.
	TypeStarted = "started"
	// TypeToken is one agent output chunk.
	TypeToken = "token"
	// TypeBackgrounded is stored when the session is demoted.
	TypeBackgrounded = "backgrounded"
	// TypeStopped is stored when the session ends.
	TypeStopped = "stopped"
)

// Event is one append-only log row.
type Event struct {
	Seq       int64
	SessionID session.ID
	Type      string
	Payload   string
	CreatedAt time.Time
}

// Log is an append-only SQLite event log in WAL mode.
type Log struct {
	db   *sql.DB
	mu   sync.Mutex
	wake map[session.ID]map[chan struct{}]struct{}
}

// Open opens (or creates) a SQLite database at path, enables WAL, and
// creates the events table. Path must be a filesystem file; :memory:
// cannot use WAL.
func Open(path string) (*Log, error) {
	if path == "" {
		return nil, fmt.Errorf("eventlog open: empty path")
	}
	dsn := "file:" + path + "?_pragma=" + url.QueryEscape("busy_timeout(5000)") +
		"&_pragma=" + url.QueryEscape("journal_mode(WAL)") +
		"&_pragma=" + url.QueryEscape("synchronous(NORMAL)")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("eventlog open: %w", err)
	}
	// One writer. Concurrent sessions queue here; G6 measures that wait.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("eventlog ping: %w", err)
	}
	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	mode, err := journalMode(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if mode != "wal" {
		_ = db.Close()
		return nil, fmt.Errorf("eventlog open: journal_mode %q, want wal", mode)
	}
	return &Log{db: db, wake: make(map[session.ID]map[chan struct{}]struct{})}, nil
}

func applySchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			foreground INTEGER NOT NULL,
			created_at_unix_nano INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			type TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at_unix_nano INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS events_session_seq ON events (session_id, seq)`,
		`CREATE TRIGGER IF NOT EXISTS events_no_update BEFORE UPDATE ON events
		 BEGIN
		  SELECT RAISE(ABORT, 'events are append-only');
		 END`,
		`CREATE TRIGGER IF NOT EXISTS events_no_delete BEFORE DELETE ON events
		 BEGIN
		  SELECT RAISE(ABORT, 'events are append-only');
		 END`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("eventlog schema: %w", err)
		}
	}
	return nil
}

func journalMode(db *sql.DB) (string, error) {
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return "", fmt.Errorf("eventlog journal_mode: %w", err)
	}
	return strings.ToLower(mode), nil
}

// JournalMode returns the active journal mode. Tests assert WAL.
func (l *Log) JournalMode() (string, error) {
	return journalMode(l.db)
}

// Close releases the database.
func (l *Log) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	if err := l.db.Close(); err != nil {
		return fmt.Errorf("eventlog close: %w", err)
	}
	return nil
}

// CreateSession inserts the session row. It does not append an event;
// the supervisor writes TypeCreated after this succeeds.
func (l *Log) CreateSession(ctx context.Context, id session.ID, status string, foreground bool) error {
	if id.IsZero() {
		return fmt.Errorf("eventlog create session: empty id")
	}
	fg := 0
	if foreground {
		fg = 1
	}
	_, err := l.db.ExecContext(ctx,
		`INSERT INTO sessions (id, status, foreground, created_at_unix_nano) VALUES (?, ?, ?, ?)`,
		id.String(), status, fg, time.Now().UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("eventlog create session: %w", err)
	}
	return nil
}

// SetSession records status and foreground. The event log is unchanged.
func (l *Log) SetSession(ctx context.Context, id session.ID, status string, foreground bool) error {
	fg := 0
	if foreground {
		fg = 1
	}
	res, err := l.db.ExecContext(ctx,
		`UPDATE sessions SET status = ?, foreground = ? WHERE id = ?`,
		status, fg, id.String(),
	)
	if err != nil {
		return fmt.Errorf("eventlog set session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("eventlog set session: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("eventlog set session: %s not found", id.String())
	}
	return nil
}

// Session is a row from the sessions table.
type Session struct {
	ID         session.ID
	Status     string
	Foreground bool
}

// GetSession returns the session row, or found=false.
func (l *Log) GetSession(ctx context.Context, id session.ID) (Session, bool, error) {
	var raw, status string
	var fg int
	err := l.db.QueryRowContext(ctx,
		`SELECT id, status, foreground FROM sessions WHERE id = ?`, id.String(),
	).Scan(&raw, &status, &fg)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("eventlog get session: %w", err)
	}
	sid, err := session.ParseID(raw)
	if err != nil {
		return Session{}, false, fmt.Errorf("eventlog get session: %w", err)
	}
	return Session{ID: sid, Status: status, Foreground: fg != 0}, true, nil
}

// Append writes one event in its own transaction and wakes tails.
func (l *Log) Append(ctx context.Context, id session.ID, typ, payload string) (Event, error) {
	events, err := l.AppendBatch(ctx, id, []Item{{Type: typ, Payload: payload}})
	if err != nil {
		return Event{}, err
	}
	return events[0], nil
}

// Item is one event in a batched write.
type Item struct {
	Type    string
	Payload string
}

// AppendBatch writes items in a single transaction. Batching is the G6
// mitigation if one-row writes stall. Callers still see one Event per item.
func (l *Log) AppendBatch(ctx context.Context, id session.ID, items []Item) ([]Event, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("eventlog append: empty id")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("eventlog append: empty batch")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("eventlog append begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO events (session_id, type, payload, created_at_unix_nano) VALUES (?, ?, ?, ?)`,
	)
	if err != nil {
		return nil, fmt.Errorf("eventlog append prepare: %w", err)
	}
	defer stmt.Close()

	out := make([]Event, 0, len(items))
	sid := id.String()
	for _, item := range items {
		if item.Type == "" {
			return nil, fmt.Errorf("eventlog append: empty type")
		}
		now := time.Now().UTC()
		res, err := stmt.ExecContext(ctx, sid, item.Type, item.Payload, now.UnixNano())
		if err != nil {
			return nil, fmt.Errorf("eventlog append: %w", err)
		}
		seq, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("eventlog append seq: %w", err)
		}
		out = append(out, Event{
			Seq:       seq,
			SessionID: id,
			Type:      item.Type,
			Payload:   item.Payload,
			CreatedAt: now,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("eventlog append commit: %w", err)
	}
	l.broadcast(id)
	return out, nil
}

// ReplayLast returns the last n events in chronological order.
func (l *Log) ReplayLast(ctx context.Context, id session.ID, n int) ([]Event, error) {
	if n < 1 {
		return nil, fmt.Errorf("eventlog replay: last %d", n)
	}
	rows, err := l.db.QueryContext(ctx, `
		SELECT seq, session_id, type, payload, created_at_unix_nano
		FROM (
			SELECT seq, session_id, type, payload, created_at_unix_nano
			FROM events
			WHERE session_id = ?
			ORDER BY seq DESC
			LIMIT ?
		) t
		ORDER BY seq ASC`, id.String(), n)
	if err != nil {
		return nil, fmt.Errorf("eventlog replay: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// After returns events with seq greater than afterSeq, chronological.
func (l *Log) After(ctx context.Context, id session.ID, afterSeq int64) ([]Event, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT seq, session_id, type, payload, created_at_unix_nano
		FROM events
		WHERE session_id = ? AND seq > ?
		ORDER BY seq ASC`, id.String(), afterSeq)
	if err != nil {
		return nil, fmt.Errorf("eventlog after: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var ev Event
		var raw string
		var nano int64
		if err := rows.Scan(&ev.Seq, &raw, &ev.Type, &ev.Payload, &nano); err != nil {
			return nil, fmt.Errorf("eventlog scan: %w", err)
		}
		id, err := session.ParseID(raw)
		if err != nil {
			return nil, fmt.Errorf("eventlog scan: %w", err)
		}
		ev.SessionID = id
		ev.CreatedAt = time.Unix(0, nano).UTC()
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eventlog scan: %w", err)
	}
	if out == nil {
		out = []Event{}
	}
	return out, nil
}

// Subscribe registers a wakeup channel for live tails. Tails must still
// read rows from SQLite; the channel is not a second log.
func (l *Log) Subscribe(id session.ID) (wait func(context.Context) error, unsub func()) {
	ch := make(chan struct{}, 1)
	l.mu.Lock()
	if l.wake[id] == nil {
		l.wake[id] = make(map[chan struct{}]struct{})
	}
	l.wake[id][ch] = struct{}{}
	l.mu.Unlock()

	unsub = func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if subs, ok := l.wake[id]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(l.wake, id)
			}
		}
	}
	wait = func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			return nil
		}
	}
	return wait, unsub
}

func (l *Log) broadcast(id session.ID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ch := range l.wake[id] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
