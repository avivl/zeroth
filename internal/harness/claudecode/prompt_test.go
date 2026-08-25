package claudecode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProposeEffectsPromptForbidsWrites(t *testing.T) {
	t.Parallel()
	for _, needle := range []string{"Do not write", `"effects"`, "unified diff", "context lines"} {
		if !strings.Contains(ProposeEffectsPrompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestProposeEffectsPromptRequiresReadBeforeDiff(t *testing.T) {
	t.Parallel()
	// 42-75: the agent invented context lines because it had no way to see
	// the file. Read is granted now, so the prompt has to say that reading
	// is required and that placeholders are not content. Losing either
	// sentence brings the guessing back.
	for _, needle := range []string{
		"Read the current full content of every file you propose to modify",
		"must come from what you just read",
		"read it to the end first",
		"Never write template or placeholder text",
	} {
		if !strings.Contains(ProposeEffectsPrompt, needle) {
			t.Fatalf("prompt missing the read-before-diff rule %q", needle)
		}
	}
	// Read is granted, but only Read.
	if !strings.Contains(ProposeEffectsPrompt, "Read is the only tool you may call") {
		t.Fatal("prompt does not scope tool use to Read")
	}
	if strings.Contains(ProposeEffectsPrompt, "Do not call tools") {
		t.Fatal("prompt still forbids all tools; Read must be permitted or diffs are guesswork")
	}
}

func TestCLIArgsGrantReadAndDenySensitivePaths(t *testing.T) {
	t.Parallel()
	args := cliArgs("do the task", "")

	// --tools Read, not "" and not a write tool.
	tools := argValue(args, "--tools")
	if tools != "Read" {
		t.Fatalf("--tools = %q, want Read", tools)
	}
	for _, banned := range []string{"Write", "Edit", "Bash", "NotebookEdit"} {
		if strings.Contains(tools, banned) {
			t.Fatalf("--tools grants %s; propose-only means no write tools", banned)
		}
	}

	// The denylist has to travel with the invocation, or Read reaches the
	// whole host filesystem (ADR-Z-0010 runs this as a host subprocess).
	settings := argValue(args, "--settings")
	if settings == "" {
		t.Fatal("--settings missing; Read would be unrestricted")
	}
	var parsed struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		t.Fatalf("--settings is not valid JSON: %v", err)
	}
	if len(parsed.Permissions.Deny) == 0 {
		t.Fatal("--settings carries no deny rules")
	}
	for _, want := range []string{".ssh", ".aws", "/etc/", ".claude.json"} {
		found := false
		for _, rule := range parsed.Permissions.Deny {
			if strings.Contains(rule, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("deny rules do not cover %q: %v", want, parsed.Permissions.Deny)
		}
	}
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
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
