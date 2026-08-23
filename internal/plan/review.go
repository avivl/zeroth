package plan

import (
	"encoding/json"
	"strings"
)

// ParseReview turns a model's free-text reply into a Review.
// Unrecognized text is a fail: a reviewer that cannot produce a
// verdict is not a pass.
func ParseReview(model, text string) Review {
	raw := stripFence(strings.TrimSpace(text))
	if raw == "" {
		return Review{Verdict: VerdictFail, Notes: "empty reviewer reply", Model: model}
	}
	if v, n, ok := reviewFromJSON(raw); ok {
		return Review{Verdict: normalizeVerdict(v), Notes: n, Model: model}
	}
	if v, n, ok := reviewFromLabeled(raw); ok {
		return Review{Verdict: normalizeVerdict(v), Notes: n, Model: model}
	}
	first, rest, _ := strings.Cut(raw, "\n")
	first = strings.TrimSpace(first)
	if isVerdictWord(first) {
		return Review{
			Verdict: normalizeVerdict(first),
			Notes:   strings.TrimSpace(rest),
			Model:   model,
		}
	}
	return Review{Verdict: VerdictFail, Notes: raw, Model: model}
}

type reviewJSON struct {
	Verdict   string `json:"verdict"`
	Notes     string `json:"notes"`
	Reasoning string `json:"reasoning"`
}

func reviewFromJSON(raw string) (verdict, notes string, ok bool) {
	var parsed reviewJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", "", false
	}
	if strings.TrimSpace(parsed.Verdict) == "" {
		return "", "", false
	}
	notes = strings.TrimSpace(parsed.Notes)
	if notes == "" {
		notes = strings.TrimSpace(parsed.Reasoning)
	}
	return parsed.Verdict, notes, true
}

func reviewFromLabeled(raw string) (verdict, notes string, ok bool) {
	var noteBuf []string
	sawNotes := false
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		key, val, cutOK := strings.Cut(trim, ":")
		if !sawNotes && cutOK && strings.EqualFold(strings.TrimSpace(key), "verdict") {
			verdict = strings.TrimSpace(val)
			ok = true
			continue
		}
		if !sawNotes && cutOK && strings.EqualFold(strings.TrimSpace(key), "notes") {
			sawNotes = true
			if rest := strings.TrimSpace(val); rest != "" {
				noteBuf = append(noteBuf, rest)
			}
			continue
		}
		if ok {
			noteBuf = append(noteBuf, line)
		}
	}
	return verdict, strings.TrimSpace(strings.Join(noteBuf, "\n")), ok
}

func isVerdictWord(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case VerdictPass, "ok", "allow", VerdictFail, "deny", "block", VerdictPassWithNotes, "pass-with-notes":
		return true
	default:
		return false
	}
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		first := strings.TrimSpace(s[:i])
		if first != "" && !strings.Contains(first, "{") && !strings.Contains(strings.ToLower(first), "verdict") {
			s = s[i+1:]
		}
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}
