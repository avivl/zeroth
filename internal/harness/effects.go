package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// effectAliases accepts G4 field names (op, target, payload) and the
// OpenAPI PlanEffect names (type, path), plus a few vendor variants.
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
	typ := canonicalizeType(firstNonEmpty(a.Op, a.Type))
	path := normalizePath(firstNonEmpty(a.Target, a.Path, a.File))
	diff := firstNonEmpty(a.Diff, a.Payload, a.Content)
	return Effect{Type: typ, Path: path, Diff: diff}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func normalizePath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	return p
}

func canonicalizeType(op string) string {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "create":
		return "create"
	case "modify", "write":
		return "modify"
	case "destroy", "delete":
		return "destroy"
	case "memory_proposal":
		return "memory_proposal"
	default:
		return strings.ToLower(strings.TrimSpace(op))
	}
}

var allowedTypes = map[string]bool{
	"create":          true,
	"modify":          true,
	"destroy":         true,
	"memory_proposal": true,
}

// ParseEffects extracts a proposed-effect set from a harness transcript.
// The result uses OpenAPI PlanEffect names so the plan builder can
// consume it without a second mapping step.
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
// and main.go targets with a body each. Kept for the offline corpus.
func ThreeFileOK(effects []Effect) bool {
	want := map[string]bool{"README.md": false, "greet.go": false, "main.go": false}
	for _, e := range effects {
		name := normalizePath(e.Path)
		if _, ok := want[name]; ok {
			if strings.TrimSpace(e.Diff) != "" {
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
		if !allowedTypes[e.Type] {
			return fmt.Errorf("harness effects: item %d: unknown type %q", i, e.Type)
		}
		if strings.TrimSpace(e.Path) == "" {
			return fmt.Errorf("harness effects: item %d: missing path", i)
		}
		if strings.TrimSpace(e.Diff) == "" {
			return fmt.Errorf("harness effects: item %d: missing diff", i)
		}
	}
	return nil
}
