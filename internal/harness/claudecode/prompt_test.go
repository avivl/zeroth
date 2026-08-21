package claudecode

import (
	"strings"
	"testing"
)

func TestProposeEffectsPromptForbidsWrites(t *testing.T) {
	t.Parallel()
	for _, needle := range []string{"Do not write", "Do not call tools", `"effects"`} {
		if !strings.Contains(ProposeEffectsPrompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestCLIArgsAreG4Shim(t *testing.T) {
	t.Parallel()
	args := cliArgs("do the task", "")
	joined := strings.Join(args, "\x00")
	for _, want := range []string{"-p", "--output-format", "stream-json", "--include-partial-messages", "--bare", "--tools", "--permission-mode", "plan", "--input-format", "stream-json", "--system-prompt"} {
		if !containsArg(args, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
	if !strings.Contains(joined, ProposeEffectsPrompt) {
		t.Fatal("system prompt not passed to claude")
	}
	if args[len(args)-1] != "do the task" {
		t.Fatalf("prompt last: %v", args)
	}
	if containsArg(args, "--resume") {
		t.Fatal("resume should be omitted when empty")
	}

	resumed := cliArgs("again", "sess-1")
	if !containsArg(resumed, "--resume") || !containsArg(resumed, "sess-1") {
		t.Fatalf("resume args: %v", resumed)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestName(t *testing.T) {
	t.Parallel()
	if got := New().Name(); got != driverName {
		t.Fatalf("Name() = %q, want %q", got, driverName)
	}
}
