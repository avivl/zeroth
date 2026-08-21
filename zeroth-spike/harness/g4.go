package harness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RunSource is how a G4 transcript was produced.
type RunSource string

const (
	SourceClaude   RunSource = "claude"
	SourceMessages RunSource = "messages"
)

// GateRun is one G4 attempt against the 3-file task.
type GateRun struct {
	Index       int
	Source      RunSource
	ParseOK     bool
	ThreeFileOK bool
	ParserAgent bool
	WroteFiles  bool
	Effects     []Effect
	Err         string
}

// GateResult is the G4 scoreboard.
type GateResult struct {
	Runs        []GateRun
	ParseOK     int
	ThreeFileOK int
	ParserAgent int
	WroteFiles  int
	Source      RunSource
	Pass        bool
	Example     []Effect
}

// RunGateG4 runs the 3-file task n times. Prefer `claude -p` when the
// binary exists; otherwise the Messages API. The API key is never logged.
func RunGateG4(ctx context.Context, n int) (GateResult, error) {
	var out GateResult
	if n < 1 {
		return out, fmt.Errorf("harness g4: n must be >= 1")
	}
	if err := APIKeyConfigured(); err != nil {
		return out, err
	}
	source := SourceMessages
	if _, err := exec.LookPath("claude"); err == nil {
		source = SourceClaude
	}
	out.Source = source
	client := messagesClient()

	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		run, err := runOnce(ctx, i+1, source, client)
		if err != nil {
			return out, err
		}
		if run.ParseOK {
			out.ParseOK++
		}
		if run.ThreeFileOK {
			out.ThreeFileOK++
			if len(out.Example) == 0 {
				out.Example = run.Effects
			}
		}
		if run.ParserAgent {
			out.ParserAgent++
		}
		if run.WroteFiles {
			out.WroteFiles++
		}
		out.Runs = append(out.Runs, run)
	}
	// Pass: 9/10 parseable effects for the 3-file change, and no writes.
	out.Pass = out.ThreeFileOK*10 >= 9*n && out.WroteFiles == 0
	return out, nil
}

func runOnce(ctx context.Context, index int, source RunSource, client *MessagesClient) (GateRun, error) {
	run := GateRun{Index: index, Source: source}
	dir, hash, err := writeThreeFileWorkspace()
	if err != nil {
		return run, err
	}
	defer os.RemoveAll(dir)

	transcript, src, err := completeOnce(ctx, source, client, dir)
	run.Source = src
	if err != nil {
		run.Err = err.Error()
		return run, nil
	}
	after, err := hashDir(dir)
	if err != nil {
		return run, err
	}
	run.WroteFiles = after != hash

	effects, usedAgent, parseErr := ParseEffectsWithAgent(ctx, transcript, client)
	run.ParserAgent = usedAgent
	if parseErr != nil {
		run.Err = parseErr.Error()
		return run, nil
	}
	run.Effects = effects
	run.ParseOK = true
	run.ThreeFileOK = ThreeFileOK(effects)
	return run, nil
}

func completeOnce(ctx context.Context, source RunSource, client *MessagesClient, workspace string) (string, RunSource, error) {
	if source == SourceClaude {
		text, err := runClaude(ctx, workspace)
		if err == nil {
			return text, SourceClaude, nil
		}
		text, msgErr := client.complete(ctx, ProposeEffectsPrompt, ThreeFileTask)
		if msgErr != nil {
			return "", SourceClaude, fmt.Errorf("claude: %v; messages: %w", err, msgErr)
		}
		return text, SourceMessages, nil
	}
	text, err := client.complete(ctx, ProposeEffectsPrompt, ThreeFileTask)
	return text, SourceMessages, err
}

func runClaude(ctx context.Context, workspace string) (string, error) {
	prompt := ProposeEffectsPrompt + "\n\n" + ThreeFileTask
	cmd := exec.CommandContext(ctx, "claude", "-p", "--output-format", "text", prompt)
	cmd.Dir = workspace
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude -p: %w", err)
	}
	return stdout.String(), nil
}

func writeThreeFileWorkspace() (dir string, hash [32]byte, err error) {
	dir, err = os.MkdirTemp("", "zeroth-spike-g4-")
	if err != nil {
		return "", hash, fmt.Errorf("harness g4 workspace: %w", err)
	}
	files := map[string]string{
		"README.md": "# demo\n",
		"greet.go":  "package greet\n\nfunc Hello() string { return \"hi\" }\n",
		"main.go": `package main

import (
	"fmt"

	"demo/greet"
)

func main() {
	fmt.Println(greet.Hello())
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			_ = os.RemoveAll(dir)
			return "", hash, fmt.Errorf("harness g4 workspace: %w", err)
		}
	}
	hash, err = hashDir(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", hash, err
	}
	return dir, hash, nil
}

func hashDir(dir string) ([32]byte, error) {
	var out [32]byte
	h := sha256.New()
	for _, name := range []string{"README.md", "greet.go", "main.go"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return out, fmt.Errorf("harness g4 hash: %w", err)
		}
		_, _ = fmt.Fprintf(h, "%s\n%d\n", name, len(body))
		_, _ = h.Write(body)
	}
	copy(out[:], h.Sum(nil))
	return out, nil
}

// FormatExample returns a compact JSON effect set for RESULTS.md.
func FormatExample(effects []Effect) string {
	raw, err := json.MarshalIndent(effects, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(raw)
}
