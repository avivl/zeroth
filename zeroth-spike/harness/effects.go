package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Effect is one proposed mutation. Field names match Linear 42-8
// (op, target, diff/payload), with aliases for the OpenAPI PlanEffect
// shape (type, path).
type Effect struct {
	Op      string `json:"op"`
	Target  string `json:"target"`
	Diff    string `json:"diff,omitempty"`
	Payload string `json:"payload,omitempty"`
}

// effectAliases accepts the OpenAPI names and a few vendor variants.
type effectAliases struct {
	Op      string `json:"op"`
	Type    string `json:"type"`
	Target  string `json:"target"`
	Path    string `json:"path"`
	File    string `json:"file"`
	Diff    string `json:"diff"`
	Payload string `json:"payload"`
	Content string `json:"content"`
}

func (a effectAliases) toEffect() Effect {
	op := firstNonEmpty(a.Op, a.Type)
	target := firstNonEmpty(a.Target, a.Path, a.File)
	diff := a.Diff
	payload := firstNonEmpty(a.Payload, a.Content)
	return Effect{Op: op, Target: target, Diff: diff, Payload: payload}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

var allowedOps = map[string]bool{
	"create":          true,
	"modify":          true,
	"destroy":         true,
	"write":           true, // vendor synonym of create/modify
	"delete":          true, // vendor synonym of destroy
	"memory_proposal": true,
}

// ParseEffects extracts a parseable effect set from a harness transcript.
// It is the deterministic parser. The parser agent is a second pass.
func ParseEffects(transcript string) ([]Effect, error) {
	raw, err := extractJSON(transcript)
	if err != nil {
		return nil, err
	}
	effects, err := unmarshalEffects(raw)
	if err != nil {
		return nil, err
	}
	if err := validateEffects(effects); err != nil {
		return nil, err
	}
	return effects, nil
}

// ThreeFileOK reports whether effects cover the G4 README, greet.go,
// and main.go targets with a body each.
func ThreeFileOK(effects []Effect) bool {
	want := map[string]bool{"README.md": false, "greet.go": false, "main.go": false}
	for _, e := range effects {
		name := strings.TrimPrefix(strings.ReplaceAll(e.Target, "\\", "/"), "./")
		if _, ok := want[name]; ok {
			if strings.TrimSpace(e.Diff) != "" || strings.TrimSpace(e.Payload) != "" {
				want[name] = true
			}
		}
	}
	for _, ok := range want {
		if !ok {
			return false
		}
	}
	return true
}

func extractJSON(transcript string) ([]byte, error) {
	s := strings.TrimSpace(transcript)
	if s == "" {
		return nil, fmt.Errorf("harness effects: empty transcript")
	}
	if inner, ok := jsonFromClaudePrint(s); ok {
		s = inner
	}
	s = stripFence(s)
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return nil, fmt.Errorf("harness effects: no JSON object")
	}
	open := s[start]
	closeCh := byte('}')
	if open == '[' {
		closeCh = ']'
	}
	end := strings.LastIndexByte(s, closeCh)
	if end < start {
		return nil, fmt.Errorf("harness effects: truncated JSON")
	}
	return []byte(s[start : end+1]), nil
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// jsonFromClaudePrint unwraps `claude -p --output-format json`.
func jsonFromClaudePrint(s string) (string, bool) {
	var wrap struct {
		Result string `json:"result"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &wrap); err != nil {
		return "", false
	}
	if strings.TrimSpace(wrap.Result) == "" {
		return "", false
	}
	return wrap.Result, true
}

func unmarshalEffects(raw []byte) ([]Effect, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("harness effects: empty JSON")
	}
	if raw[0] == '[' {
		var aliases []effectAliases
		if err := json.Unmarshal(raw, &aliases); err != nil {
			return nil, fmt.Errorf("harness effects: %w", err)
		}
		return aliasesToEffects(aliases), nil
	}
	var env struct {
		Effects []effectAliases `json:"effects"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("harness effects: %w", err)
	}
	if len(env.Effects) == 0 {
		return nil, fmt.Errorf("harness effects: missing effects array")
	}
	return aliasesToEffects(env.Effects), nil
}

func aliasesToEffects(aliases []effectAliases) []Effect {
	out := make([]Effect, 0, len(aliases))
	for _, a := range aliases {
		out = append(out, a.toEffect())
	}
	return out
}

func validateEffects(effects []Effect) error {
	if len(effects) == 0 {
		return fmt.Errorf("harness effects: empty list")
	}
	for i, e := range effects {
		op := strings.ToLower(strings.TrimSpace(e.Op))
		if !allowedOps[op] {
			return fmt.Errorf("harness effects: item %d: unknown op %q", i, e.Op)
		}
		if strings.TrimSpace(e.Target) == "" {
			return fmt.Errorf("harness effects: item %d: missing target", i)
		}
		if strings.TrimSpace(e.Diff) == "" && strings.TrimSpace(e.Payload) == "" {
			return fmt.Errorf("harness effects: item %d: missing diff/payload", i)
		}
	}
	return nil
}
