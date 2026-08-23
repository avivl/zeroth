package docker

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/avivl/zeroth/internal/sandbox"
)

func TestExportWorkspaceFailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	key := "AKI" + "AZEROTHTESTFAKE01"
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(key+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := exportWorkspace(t.Context(), dir, &buf)
	if !errors.Is(err, sandbox.ErrSecret) {
		t.Fatalf("exportWorkspace: %v, want ErrSecret", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("fail-closed wrote %d bytes", buf.Len())
	}
}

func TestExportWorkspaceOmitsThenScans(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	key := "AKI" + "AZEROTHTESTFAKE01"
	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git-credentials"), []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := exportWorkspace(t.Context(), dir, &buf); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte(key)) {
		t.Fatal("excluded secret still in export")
	}
	out := t.TempDir()
	if err := sandbox.UnpackOverlay(out, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, ".git-credentials")); err == nil {
		t.Fatal("excluded path survived export")
	}
}
