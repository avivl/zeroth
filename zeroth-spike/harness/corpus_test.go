package harness_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/avivl/zeroth/zeroth-spike/harness"
)

func TestParseEffectsCorpus(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "testdata", "g4")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 10 {
		t.Fatalf("corpus size %d, want at least 10 transcripts", len(entries))
	}
	okCount := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		effects, err := harness.ParseEffects(string(body))
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		if !harness.ThreeFileOK(effects) {
			t.Errorf("%s: parsed but not 3-file: %+v", e.Name(), effects)
			continue
		}
		okCount++
	}
	if okCount < 9 {
		t.Fatalf("corpus parseable %d/%d, bar 9/10", okCount, len(entries))
	}
}
