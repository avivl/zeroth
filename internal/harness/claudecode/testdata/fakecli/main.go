// Fake Claude Code CLI for harness conformance. Speaks stream-json NDJSON
// on stdout. Never writes credentials to the workspace.
package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if os.Getenv("ZEROTH_FAKE_PID") != "" {
		_ = os.WriteFile(".fake-pid", []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	if path := os.Getenv("ZEROTH_FAKE_HOST_WRITE"); path != "" {
		_ = os.WriteFile(path, []byte("host-subprocess"), 0o644)
	}
	if os.Getenv("ZEROTH_FAKE_EXIT_BEFORE_STDIN") != "" {
		_ = os.Stdin.Close()
		os.Exit(0)
	}

	prompt := ""
	if n := len(os.Args); n > 0 {
		prompt = os.Args[n-1]
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigc
		os.Exit(0)
	}()

	writeLine(sysInit{Type: "system", Subtype: "init", SessionID: "fake-session-1"})

	switch {
	case strings.Contains(prompt, "SLEEP"):
		// spawn() always writes the first user message to stdin. Read it
		// before sleeping so that write cannot hit EPIPE.
		_ = readUserText()
		emitDelta("sleep-token")
		time.Sleep(60 * time.Second)
		emitResult()
	case strings.Contains(prompt, "HOLD"):
		emitDelta("hello-token")
		if steered := readSteer(prompt); steered {
			emitDelta("steered-ok")
		}
		emitTool()
		emitResult()
	case strings.Contains(prompt, "STDINPROMPT"):
		if text := readUserText(); text != "" {
			emitDelta("from-stdin")
		}
		emitTool()
		emitResult()
	default:
		// Fast-exit paths used to emit and return without reading stdin.
		// The driver writes the first prompt after Start; if this process
		// has already exited, that write fails with broken pipe and
		// stop_idempotent flakes under load.
		_ = readUserText()
		emitDelta("hello-token")
		emitTool()
		emitResult()
	}
}

type sysInit struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
}

type streamEventLine struct {
	Type  string            `json:"type"`
	Event contentBlockDelta `json:"event"`
}

type contentBlockDelta struct {
	Type  string    `json:"type"`
	Delta textDelta `json:"delta"`
}

type textDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type assistantLine struct {
	Type    string           `json:"type"`
	Message assistantMessage `json:"message"`
}

type assistantMessage struct {
	Content []toolBlock `json:"content"`
}

type toolBlock struct {
	Type  string    `json:"type"`
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Input toolInput `json:"input"`
}

type toolInput struct {
	Path string `json:"path"`
}

type resultLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	SessionID string `json:"session_id"`
}

func readSteer(initialPrompt string) bool {
	r := bufio.NewReader(os.Stdin)
	for {
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			os.Exit(1)
		}
		text := userText(line)
		if text != "" && text != initialPrompt {
			return true
		}
		if err == io.EOF {
			return false
		}
	}
}

func readUserText() string {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		os.Exit(1)
	}
	return userText(line)
}

func userText(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	var msg struct {
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return line
	}
	var b strings.Builder
	for _, p := range msg.Message.Content {
		if p.Type == "text" || p.Type == "" {
			b.WriteString(p.Text)
		}
	}
	if b.Len() > 0 {
		return b.String()
	}
	return line
}

func emitDelta(text string) {
	writeLine(streamEventLine{
		Type: "stream_event",
		Event: contentBlockDelta{
			Type:  "content_block_delta",
			Delta: textDelta{Type: "text_delta", Text: text},
		},
	})
}

func emitTool() {
	writeLine(assistantLine{
		Type: "assistant",
		Message: assistantMessage{
			Content: []toolBlock{{
				Type:  "tool_use",
				ID:    "tu_1",
				Name:  "Read",
				Input: toolInput{Path: "README.md"},
			}},
		},
	})
}

func emitResult() {
	writeLine(resultLine{
		Type:      "result",
		Subtype:   "success",
		Result:    `{"effects":[{"op":"modify","target":"README.md","diff":"+Version: 2"}]}`,
		IsError:   false,
		SessionID: "fake-session-1",
	})
}

func writeLine[T any](v T) {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(v); err != nil {
		os.Exit(1)
	}
	_ = os.Stdout.Sync()
}
