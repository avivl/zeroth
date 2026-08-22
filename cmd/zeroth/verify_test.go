package main

import (
	"bytes"
	"database/sql"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

func TestVerifyOfflineCleanRun(t *testing.T) {
	t.Parallel()
	dbPath, runID := signedRun(t)
	var out bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"verify", "--db-path", dbPath, runID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify clean: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "ok") || !strings.Contains(out.String(), runID) {
		t.Fatalf("verify output: %s", out.String())
	}
}

func TestVerifyTamperNamesRecord(t *testing.T) {
	t.Parallel()
	dbPath, runID := signedRun(t)

	db, err := openRawSQLite(t, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var recID string
	if err := db.QueryRow(`SELECT id FROM audit_records WHERE session_id = ? LIMIT 1`, runID).Scan(&recID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS audit_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE audit_records SET action = 'evil' WHERE id = ?`, recID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"verify", "--db-path", dbPath, runID})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected tamper to fail verify")
	}
	msg := err.Error() + out.String()
	if !strings.Contains(msg, recID) {
		t.Fatalf("tamper should name record %s: %s", recID, msg)
	}
}

func TestVerifyMissingMiddleNamesWhere(t *testing.T) {
	t.Parallel()
	dbPath, ids := signedRuns(t, 2)
	runID := ids[0]

	db, err := openRawSQLite(t, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id FROM audit_records ORDER BY created_at_unix_nano ASC, id ASC`)
	if err != nil {
		t.Fatal(err)
	}
	var recIDs []string
	for rows.Next() {
		var one string
		if err := rows.Scan(&one); err != nil {
			t.Fatal(err)
		}
		recIDs = append(recIDs, one)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(recIDs) < 3 {
		t.Fatalf("need at least 3 records, got %d", len(recIDs))
	}
	mid := recIDs[1]
	after := recIDs[2]
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS audit_no_delete`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM audit_records WHERE id = ?`, mid); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"verify", "--db-path", dbPath, runID})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected gap to fail verify")
	}
	msg := err.Error() + out.String()
	if !strings.Contains(msg, "chain broken") || !strings.Contains(msg, after) {
		t.Fatalf("gap should name where (%s): %s", after, msg)
	}
}

// signedRun starts a server, creates a run, then shuts everything down so
// verify has to work against the SQLite file with no daemon.
func signedRun(t *testing.T) (dbPath, runID string) {
	t.Helper()
	dbPath, ids := signedRuns(t, 1)
	return dbPath, ids[0]
}

func signedRuns(t *testing.T, n int) (dbPath string, runIDs []string) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "zeroth.db")
	st, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: time.Millisecond,
		TokenCount:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(srv.Handler())
	for i := 0; i < n; i++ {
		var out bytes.Buffer
		run := newRoot()
		run.SetOut(&out)
		run.SetErr(io.Discard)
		run.SetArgs([]string{"--addr", strings.TrimPrefix(hs.URL, "http://"), "run", "verify-task"})
		if err := run.Execute(); err != nil {
			t.Fatalf("run: %v\n%s", err, out.String())
		}
		id := strings.TrimSpace(out.String())
		if id == "" {
			t.Fatal("empty run id")
		}
		runIDs = append(runIDs, id)
	}
	hs.Close()
	srv.Close()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath, runIDs
}

func openRawSQLite(t *testing.T, path string) (*sql.DB, error) {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
