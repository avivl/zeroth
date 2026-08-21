package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHashTreeIgnoresMtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := HashTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	b, err := HashTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("mtime change should not affect hash")
	}
	if err := os.WriteFile(path, []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := HashTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("content change should affect hash")
	}
}

func TestPercentile(t *testing.T) {
	t.Parallel()
	ds := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second, 10 * time.Second}
	if got := Percentile(ds, 0.50); got != 3*time.Second {
		t.Fatalf("p50 = %s", got)
	}
	if got := Percentile(ds, 0.95); got != 10*time.Second {
		t.Fatalf("p95 = %s", got)
	}
	if got := Percentile(nil, 0.50); got != 0 {
		t.Fatalf("empty = %s", got)
	}
}
