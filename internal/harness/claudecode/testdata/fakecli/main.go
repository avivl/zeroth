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
		emitDelta("sleep-token")
		time.Sleep(60 * time.Second)
		emitResult()
	case strings.Contains(prompt, "HOLD"):
		emitDelta("hello-token")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			os.Exit(1)
		}
		if strings.TrimSpace(line) != "" {
			emitDelta("steered-ok")
		}
		emitTool()
		emitResult()
	default:
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
