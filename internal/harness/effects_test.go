package harness_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/harness"
)

func TestParseEffectsJSONObject(t *testing.T) {
	t.Parallel()
	raw := `{
  "effects": [
    {"op":"modify","target":"README.md","diff":"+Version: 2"},
    {"op":"modify","target":"greet.go","payload":"package greet\nfunc Greet(name string) string { return \"hello, \"+name }"},
    {"op":"modify","target":"main.go","diff":"-Hello()\n+Greet(\"zeroth\")"}
  ]
}`
	got, err := harness.ParseEffects(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !harness.ThreeFileOK(got) {
		t.Fatalf("three-file check failed: %+v", got)
	}
	if got[0].Type != "modify" || got[0].Path != "README.md" {
		t.Fatalf("OpenAPI names: %+v", got[0])
	}
}

func TestParseEffectsMarkdownFenceAndAliases(t *testing.T) {
	t.Parallel()
	raw := "Here you go:\n```json\n{\"effects\":[{\"type\":\"modify\",\"path\":\"README.md\",\"content\":\"# demo\\nVersion: 2\\n\"},{\"op\":\"modify\",\"target\":\"greet.go\",\"diff\":\"+Greet\"},{\"op\":\"modify\",\"target\":\"main.go\",\"diff\":\"+Greet\"}]}\n```\n"
	got, err := harness.ParseEffects(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !harness.ThreeFileOK(got) {
		t.Fatalf("three-file check failed: %+v", got)
	}
	if got[0].Type != "modify" || got[0].Path != "README.md" || got[0].Diff == "" {
		t.Fatalf("alias mapping: %+v", got[0])
	}
}

func TestParseEffectsArray(t *testing.T) {
	t.Parallel()
	raw := `[{"op":"modify","target":"README.md","diff":"x"},{"op":"modify","target":"greet.go","diff":"y"},{"op":"modify","target":"main.go","diff":"z"}]`
	got, err := harness.ParseEffects(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !harness.ThreeFileOK(got) {
		t.Fatalf("three-file check failed: %+v", got)
	}
}

func TestParseEffectsClaudePrintWrapper(t *testing.T) {
	t.Parallel()
	inner := `{"effects":[{"op":"modify","target":"README.md","diff":"a"},{"op":"modify","target":"greet.go","diff":"b"},{"op":"modify","target":"main.go","diff":"c"}]}`
	wrap := `{"type":"result","result":` + jsonString(inner) + `}`
	got, err := harness.ParseEffects(wrap)
	if err != nil {
		t.Fatal(err)
	}
	if !harness.ThreeFileOK(got) {
		t.Fatalf("three-file check failed: %+v", got)
	}
}

func TestParseEffectsWriteBecomesModify(t *testing.T) {
	t.Parallel()
	got, err := harness.ParseEffects(`{"effects":[{"op":"write","file":"README.md","payload":"x"},{"op":"delete","target":"gone.txt","diff":"removed"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Type != "modify" || got[0].Path != "README.md" {
		t.Fatalf("write alias: %+v", got[0])
	}
	if got[1].Type != "destroy" || got[1].Path != "gone.txt" {
		t.Fatalf("delete alias: %+v", got[1])
	}
}

func TestParseEffectsRejectsMissingBody(t *testing.T) {
	t.Parallel()
	_, err := harness.ParseEffects(`{"effects":[{"op":"modify","target":"README.md"}]}`)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "diff") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseEffectsRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := harness.ParseEffects("no json here"); err == nil {
		t.Fatal("expected error")
	}
}

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

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
