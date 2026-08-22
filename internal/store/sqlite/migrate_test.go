package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/avivl/zeroth/internal/store"
	_ "modernc.org/sqlite"
)

func TestNewRejectsEmptyAndMemory(t *testing.T) {
	t.Parallel()
	if _, err := New(""); err == nil {
		t.Fatal("empty path")
	}
	if _, err := New(":memory:"); err == nil {
		t.Fatal(":memory:")
	}
}

func TestWALMode(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	mode, err := s.JournalMode()
	if err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestSecondOpenReadsFirstWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "zeroth.db")
	ctx := t.Context()

	a, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	agent := store.Agent{ID: mustAID(t, "a1"), Name: "n", Harness: "h", Status: "ready"}
	if err := a.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	got, err := b.GetAgent(ctx, agent.ID)
	if err != nil || got.Name != "n" {
		t.Fatalf("second open: %+v err=%v", got, err)
	}
}

func TestEventsAndAuditAreAppendOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "zeroth.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	agent := store.Agent{ID: mustAID(t, "a1"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: mustSID(t, "s1"), AgentID: agent.ID, Status: "running"}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(ctx, sess.ID, store.Event{Type: "log", Message: "keep"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAudit(ctx, store.AuditRecord{
		ID: mustAuID(t, "u1"), Action: "x", ResourceType: "run", ResourceID: "s1",
		Signature: "sig", Hash: "h1", AgentPubKey: "pk",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAgentKey(ctx, store.AgentKey{AgentID: agent.ID, PubKey: "pk"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`UPDATE events SET message = 'mutated'`); err == nil {
		t.Fatal("UPDATE events: expected append-only abort")
	}
	if _, err := db.Exec(`DELETE FROM events`); err == nil {
		t.Fatal("DELETE events: expected append-only abort")
	}
	if _, err := db.Exec(`UPDATE audit_records SET action = 'mutated'`); err == nil {
		t.Fatal("UPDATE audit: expected append-only abort")
	}
	if _, err := db.Exec(`DELETE FROM audit_records`); err == nil {
		t.Fatal("DELETE audit: expected append-only abort")
	}
	if _, err := db.Exec(`DELETE FROM agent_keys`); err == nil {
		t.Fatal("DELETE agent_keys: expected append-only abort")
	}
}

func TestMigrateUpAndDown(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx := t.Context()
	v, err := s.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 5 {
		t.Fatalf("version = %d, want 5", v)
	}
	agent := store.Agent{ID: mustAID(t, "a1"), Name: "n", Harness: "h", Status: "ready"}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateDown(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = s.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 4 {
		t.Fatalf("after 0005 down version = %d", v)
	}
	if err := s.MigrateDown(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = s.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 3 {
		t.Fatalf("after 0004 down version = %d", v)
	}
	if err := s.MigrateDown(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = s.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 2 {
		t.Fatalf("after 0003 down version = %d", v)
	}
	if err := s.MigrateDown(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = s.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("after 0002 down version = %d", v)
	}
	if !agentRowExists(t, s, ctx, agent.ID) {
		t.Fatal("agent lost on additive 0002 down")
	}
	if err := s.MigrateDown(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = s.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Fatalf("after 0001 down version = %d", v)
	}
	if _, err := s.GetAgent(ctx, agent.ID); err == nil {
		t.Fatal("agent survived 0001 down")
	}
	if err := s.MigrateUp(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = s.Version(ctx)
	if err != nil || v != 5 {
		t.Fatalf("after up version = %d err=%v", v, err)
	}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("re-create after up: %v", err)
	}
}

func TestMigrateDownPreservesEarlierRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	migDir := filepath.Join(dir, "mig")
	if err := os.Mkdir(migDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyEmbed(t, migDir, "0001_init.up.sql")
	copyEmbed(t, migDir, "0001_init.down.sql")
	copyFile(t, "testdata/migrate/0002_session_note.up.sql", filepath.Join(migDir, "0002_session_note.up.sql"))
	copyFile(t, "testdata/migrate/0002_session_note.down.sql", filepath.Join(migDir, "0002_session_note.down.sql"))

	s, err := open(filepath.Join(dir, "zeroth.db"), os.DirFS(migDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := t.Context()
	v, err := s.Version(ctx)
	if err != nil || v != 2 {
		t.Fatalf("version = %d err=%v, want 2", v, err)
	}

	agentID := mustAID(t, "a1")
	if err := insertAgentV1(ctx, s, agentID); err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: mustSID(t, "s1"), AgentID: agentID, Status: "running", Prompt: "keep-me"}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET note = 'hello' WHERE id = ?`, sess.ID.String()); err != nil {
		t.Fatalf("set note: %v", err)
	}

	if err := s.MigrateDown(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = s.Version(ctx)
	if err != nil || v != 1 {
		t.Fatalf("after 0002 down version = %d err=%v", v, err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "keep-me" {
		t.Fatalf("data loss on 0002 down: %+v", got)
	}
	var note sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT note FROM sessions WHERE id = ?`, sess.ID.String()).Scan(&note)
	if err == nil {
		t.Fatal("note column still present after down")
	}
}

func agentRowExists(t *testing.T, s *Store, ctx context.Context, id store.AgentID) bool {
	t.Helper()
	var got string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM agents WHERE id = ?`, id.String()).Scan(&got)
	return err == nil && got == id.String()
}

func insertAgentV1(ctx context.Context, s *Store, id store.AgentID) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, name, harness, status, created_at_unix_nano, updated_at_unix_nano)
		VALUES (?, 'n', 'h', 'ready', 1, 1)`, id.String())
	return err
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func copyEmbed(t *testing.T, destDir, name string) {
	t.Helper()
	body, err := embeddedMigrations.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, src, dest string) {
	t.Helper()
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustAID(t testing.TB, raw string) store.AgentID {
	t.Helper()
	id, err := store.ParseAgentID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSID(t testing.TB, raw string) store.SessionID {
	t.Helper()
	id, err := store.ParseSessionID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustAuID(t testing.TB, raw string) store.AuditID {
	t.Helper()
	id, err := store.ParseAuditID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLoadMigrationsRejectsMissingDown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "0001_only.up.sql"), []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMigrations(os.DirFS(dir)); err == nil {
		t.Fatal("expected missing down error")
	}
}
