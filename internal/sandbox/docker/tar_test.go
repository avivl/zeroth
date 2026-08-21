package docker

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
