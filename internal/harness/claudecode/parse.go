package claudecode

import (
	"encoding/json"
	"strings"

	"github.com/avivl/zeroth/internal/harness"
)

type streamLine struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	Message   json.RawMessage `json:"message"`
	Event     json.RawMessage `json:"event"`
}

type assistantMessage struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type anthropicEvent struct {
	Type         string        `json:"type"`
	Delta        *deltaBlock   `json:"delta"`
	ContentBlock *contentBlock `json:"content_block"`
}

type deltaBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type extractor struct {
	key      string
	sawDelta bool
	session  string
}

func (e *extractor) handleLine(line string) []harness.Event {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var sl streamLine
	if err := json.Unmarshal([]byte(line), &sl); err != nil {
		return []harness.Event{{Kind: harness.EventToken, Payload: e.redact(line)}}
	}
	if sl.SessionID != "" {
		e.session = sl.SessionID
	}
	switch sl.Type {
	case "system":
		return nil
	case "stream_event":
		if len(sl.Event) == 0 {
			return nil
		}
		return e.handleAnthropicEvent(sl.Event)
	case "content_block_delta", "content_block_start":
		return e.handleAnthropicEvent([]byte(line))
	case "assistant":
		return e.handleAssistant(sl.Message)
	case "result":
		return e.handleResult(sl)
	case "error":
		msg := sl.Result
		if msg == "" {
			msg = "vendor error"
		}
		return []harness.Event{{Kind: harness.EventError, Payload: e.redact(msg)}}
	default:
		return nil
	}
}

func (e *extractor) handleAnthropicEvent(raw []byte) []harness.Event {
	var ev anthropicEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil
	}
	switch ev.Type {
	case "content_block_delta":
		if ev.Delta != nil && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
			e.sawDelta = true
			return []harness.Event{{Kind: harness.EventToken, Payload: e.redact(ev.Delta.Text)}}
		}
	case "content_block_start":
		if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
			return e.toolCall(*ev.ContentBlock)
		}
	}
	return nil
}

func (e *extractor) handleAssistant(raw json.RawMessage) []harness.Event {
	if len(raw) == 0 {
		return nil
	}
	var msg assistantMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil
	}
	var out []harness.Event
	var text strings.Builder
	for _, b := range msg.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			out = append(out, e.toolCall(b)...)
		}
	}
	if !e.sawDelta && text.Len() > 0 {
		out = append(out, harness.Event{Kind: harness.EventToken, Payload: e.redact(text.String())})
	}
	e.sawDelta = false
	return out
}

func (e *extractor) handleResult(sl streamLine) []harness.Event {
	if sl.IsError {
		msg := sl.Result
		if msg == "" {
			msg = "vendor result error"
		}
		return []harness.Event{{Kind: harness.EventError, Payload: e.redact(msg)}}
	}
	body := sl.Result
	if strings.TrimSpace(body) == "" {
		return nil
	}
	effects, err := harness.ParseEffects(body)
	if err != nil {
		effects, err = harness.ParseEffects(e.redact(body))
	}
	if err != nil {
		return []harness.Event{{Kind: harness.EventToken, Payload: e.redact(body)}}
	}
	return []harness.Event{{Kind: harness.EventEffects, Effects: effects}}
}

func (e *extractor) toolCall(b contentBlock) []harness.Event {
	type call struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input,omitempty"`
	}
	raw, err := json.Marshal(call{ID: b.ID, Name: b.Name, Input: b.Input})
	if err != nil {
		return nil
	}
	payload := e.redact(string(raw))
	if payload == "" {
		return nil
	}
	return []harness.Event{{Kind: harness.EventToolCall, Payload: payload}}
}

func (e *extractor) redact(s string) string {
	return redact(s, e.key)
}
