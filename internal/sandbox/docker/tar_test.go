package docker

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/sandbox"
)

func TestSafeJoinRejectsEscape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := safeJoin(dir, "../escape.txt"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := safeJoin(dir, "foo/../../escape.txt"); err == nil {
		t.Fatal("expected nested escape error")
	}
	got, err := safeJoin(dir, "/abs.txt")
	if err != nil {
		t.Fatalf("absolute should be stripped, got %v", err)
	}
	if filepath.Base(got) != "abs.txt" {
		t.Fatalf("got %s", got)
	}
	rel, err := filepath.Rel(dir, got)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("joined outside dest: %s", got)
	}
	got, err = safeJoin(dir, "ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "ok.txt" {
		t.Fatalf("got %s", got)
	}
}

func TestUnpackTarRejectsEscape(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, "nope"); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := unpackTar(dir, bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); err == nil {
		t.Fatal("escaped file was written")
	}
}

func TestPackUnpackRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := "hello workspace\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := packTar(t.Context(), dir, &buf); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := unpackTar(out, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("got %q", got)
	}
}

func TestPackTarOmitsExcluded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git-credentials"), []byte("https://user:token@github.com"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".config", "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".config", "gh", "hosts.yml"), []byte("github.com:"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := packTar(t.Context(), dir, &buf); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("user:token")) {
		t.Fatal("packed excluded credential content")
	}
	out := t.TempDir()
	if err := unpackTar(out, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "kept.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, ".git-credentials")); err == nil {
		t.Fatal("excluded .git-credentials was packed")
	}
	if _, err := os.Stat(filepath.Join(out, ".config", "gh", "hosts.yml")); err == nil {
		t.Fatal("excluded gh hosts.yml was packed")
	}
}

func TestUnpackTarSkipsExcluded(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range []string{"kept.txt", ".git-credentials"} {
		body := name + "-body"
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := unpackTar(dir, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "kept.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "kept.txt-body" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git-credentials")); err == nil {
		t.Fatal("unpack restored excluded credentials")
	}
}

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
	if err := unpackTar(out, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, ".git-credentials")); err == nil {
		t.Fatal("excluded path survived export")
	}
}
