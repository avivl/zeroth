package plan

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// bigReadme builds a file of the shape that broke in the field: an image
// banner first (so a diff anchored on a "# Title" first line is wrong), a
// few hundred body lines, and a distinctive last line.
func bigReadme() []byte {
	var b strings.Builder
	b.WriteString("![Zeroth](zeroth-app-icon.svg )\n\n# Zeroth\n\n")
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&b, "Body line %d of the readme, filler to make this a realistic length.\n", i)
	}
	b.WriteString("\n## Status\n\nStage 1 in progress. Final line marker: ZETA-END.\n")
	return []byte(b.String())
}

// 42-75: appending to the end of a long file is the shape that failed
// twice in real runs. It needs the true last line and its true line
// number, which is exactly what an agent that cannot read the file has to
// invent.
func TestEOFAnchoredAppend(t *testing.T) {
	t.Parallel()
	original := bigReadme()
	lines := strings.Split(strings.TrimSuffix(string(original), "\n"), "\n")
	last := lines[len(lines)-1]
	startLine := len(lines) - 2 // anchor on the final three lines

	good := fmt.Sprintf(
		"--- a/README.md\n+++ b/README.md\n@@ -%d,3 +%d,7 @@\n %s\n %s\n %s\n+\n+## License\n+\n+MIT. See [LICENSE](./LICENSE).\n",
		startLine, startLine, lines[len(lines)-3], lines[len(lines)-2], last)

	out, err := applyModify(original, good)
	if err != nil {
		t.Fatalf("a correct EOF-anchored append was rejected: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "## License") {
		t.Fatalf("append did not land: tail=%q", tail(got))
	}
	if !strings.Contains(got, last) {
		t.Fatal("append destroyed the original final line")
	}
	if !strings.HasPrefix(got, "![Zeroth](zeroth-app-icon.svg )") {
		t.Fatalf("append disturbed the head of the file: %q", got[:60])
	}
	if strings.Index(got, "## License") < strings.Index(got, last) {
		t.Fatal("new section landed before the old final line, not at EOF")
	}
	// Nothing but the addition: same bytes plus the new section.
	if len(out) <= len(original) {
		t.Fatalf("file did not grow: %d -> %d", len(original), len(out))
	}
}

// The two failure shapes from the ticket, against the same file. Both must
// be refused, and the placeholder one must say so plainly.
func TestEOFAppendRejectsFieldFailures(t *testing.T) {
	t.Parallel()
	original := bigReadme()

	t.Run("hallucinated first line, attempt 1", func(t *testing.T) {
		t.Parallel()
		diff := "--- a/README.md\n+++ b/README.md\n@@ -1,3 +1,7 @@\n # Project\n \n Some intro.\n+\n+## License\n"
		_, err := applyModify(original, diff)
		if err == nil {
			t.Fatal("a diff anchored on an invented first line was accepted")
		}
		if !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("err = %v, want a hunk conflict", err)
		}
	})

	t.Run("unresolved placeholder, attempt 3", func(t *testing.T) {
		t.Parallel()
		diff := "--- a/README.md\n+++ b/README.md\n@@ -1,1 +1,5 @@ EOF-anchored append\n<<last line of README.md>>\n+\n+## License\n\n+MIT. See [LICENSE](./LICENSE) for the full text.\n"
		_, err := applyModify(original, diff)
		if err == nil {
			t.Fatal("a diff with placeholder context was accepted")
		}
		if !errors.Is(err, ErrPlaceholderDiff) {
			t.Fatalf("err = %v, want ErrPlaceholderDiff so this is diagnosable without a trace read", err)
		}
	})
}

func tail(s string) string {
	if len(s) < 200 {
		return s
	}
	return s[len(s)-200:]
}
