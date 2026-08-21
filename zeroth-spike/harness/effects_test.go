package harness_test

import (
	"strings"
	"testing"

	"github.com/avivl/zeroth/zeroth-spike/harness"
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
	if got[0].Op != "modify" || got[0].Target != "README.md" || got[0].Payload == "" {
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

func TestParseEffectsRejectsMissingBody(t *testing.T) {
	t.Parallel()
	_, err := harness.ParseEffects(`{"effects":[{"op":"modify","target":"README.md"}]}`)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "diff/payload") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseEffectsRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := harness.ParseEffects("no json here"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProposeEffectsPromptForbidsWrites(t *testing.T) {
	t.Parallel()
	p := harness.ProposeEffectsPrompt
	for _, needle := range []string{"Do not write", "Do not call tools", `"effects"`} {
		if !strings.Contains(p, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
	if !strings.Contains(harness.ThreeFileTask, "README.md") {
		t.Fatal("task missing README.md")
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
