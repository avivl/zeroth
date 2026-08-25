package plan

import (
	"errors"
	"strings"
	"testing"
)

// 42-75: the agent intermittently emits a diff whose context lines are
// unresolved template text rather than real file content. The generic
// hunk-conflict error made that indistinguishable from an ordinary stale
// diff, and diagnosing it needed a raw-payload trace read.

func TestApplyModifyRejectsPlaceholderDiff(t *testing.T) {
	t.Parallel()
	original := []byte("![Zeroth](zeroth-app-icon.svg )\n\n# Zeroth\n\nbody\n")

	cases := []struct {
		name string
		diff string
	}{
		{
			// Verbatim from the ticket's attempt 3.
			name: "angle placeholder in context line",
			diff: "--- a/README.md\n+++ b/README.md\n@@ -1,1 +1,5 @@ EOF-anchored append\n<<last line of README.md>>\n+\n+## License\n",
		},
		{
			// Verbatim from my reproduction with the shipped no-tools flags.
			name: "angle placeholder standing in for the file body",
			diff: "--- a/README.md\n+++ b/README.md\n@@ -1,3 +1,7 @@\n # README\n \n <existing content preserved>\n+\n+## License\n",
		},
		{
			name: "bracket placeholder",
			diff: "--- a/README.md\n+++ b/README.md\n@@ -1,2 +1,4 @@\n [existing content here]\n+\n+## License\n",
		},
		{
			name: "ellipsis stand-in",
			diff: "--- a/README.md\n+++ b/README.md\n@@ -1,2 +1,4 @@\n ... rest of file unchanged ...\n+\n+## License\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := applyModify(original, tc.diff)
			if err == nil {
				t.Fatal("placeholder diff was accepted")
			}
			if !errors.Is(err, ErrPlaceholderDiff) {
				t.Fatalf("err = %v, want ErrPlaceholderDiff", err)
			}
			// The message has to name the offending text, or this is no
			// more diagnosable than the generic conflict it replaces.
			if !strings.Contains(err.Error(), "placeholder") {
				t.Fatalf("err = %v, want it to say placeholder", err)
			}
		})
	}
}

func TestApplyModifyAcceptsRealDiffsThatMentionBrackets(t *testing.T) {
	t.Parallel()
	// Markdown links and Go generics are ordinary content. Detection must
	// not fire on a diff whose context genuinely matches the file.
	original := []byte("# Docs\n\nSee [LICENSE](./LICENSE) for terms.\n\nfunc F[T any](v T) {}\n")
	diff := "--- a/README.md\n+++ b/README.md\n@@ -1,5 +1,7 @@\n # Docs\n \n See [LICENSE](./LICENSE) for terms.\n \n func F[T any](v T) {}\n+\n+## License\n"

	out, err := applyModify(original, diff)
	if err != nil {
		t.Fatalf("real diff rejected: %v", err)
	}
	if !strings.Contains(string(out), "## License") {
		t.Fatalf("patch did not apply: %q", out)
	}
	if !strings.Contains(string(out), "See [LICENSE](./LICENSE) for terms.") {
		t.Fatalf("original content lost: %q", out)
	}
}
