package memory

import (
	"sort"
	"strings"
	"time"
)

func sortFacts(facts []Fact) {
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Kind != facts[j].Kind {
			return facts[i].Kind < facts[j].Kind
		}
		if facts[i].RefID != facts[j].RefID {
			return facts[i].RefID < facts[j].RefID
		}
		return facts[i].Key < facts[j].Key
	})
}

func sortProposals(ps []Proposal) {
	sort.Slice(ps, func(i, j int) bool {
		if !ps[i].CreatedAt.Equal(ps[j].CreatedAt) {
			return ps[i].CreatedAt.After(ps[j].CreatedAt)
		}
		return ps[i].ID.String() > ps[j].ID.String()
	})
}

// SplitKeyBody reads an optional leading "key: name" line from content.
// When absent, key is empty and the caller must supply one.
func SplitKeyBody(content string) (key, body string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ""
	}
	line, rest, found := strings.Cut(content, "\n")
	line = strings.TrimSpace(line)
	if len(line) >= 4 && strings.EqualFold(line[:4], "key:") {
		key = strings.TrimSpace(line[4:])
		if found {
			return key, strings.TrimSpace(rest)
		}
		return key, ""
	}
	return "", content
}

func unified(before, after string) string {
	a := splitLines(before)
	b := splitLines(after)
	pre := commonPrefix(a, b)
	a = a[pre:]
	b = b[pre:]
	suf := commonSuffix(a, b)
	a = a[:len(a)-suf]
	b = b[:len(b)-suf]
	var bld strings.Builder
	for i := 0; i < pre; i++ {
		bld.WriteString(" ")
		bld.WriteString(splitLines(before)[i])
		bld.WriteByte('\n')
	}
	for _, line := range a {
		bld.WriteString("-")
		bld.WriteString(line)
		bld.WriteByte('\n')
	}
	for _, line := range b {
		bld.WriteString("+")
		bld.WriteString(line)
		bld.WriteByte('\n')
	}
	if suf > 0 {
		tail := splitLines(after)
		for _, line := range tail[len(tail)-suf:] {
			bld.WriteString(" ")
			bld.WriteString(line)
			bld.WriteByte('\n')
		}
	}
	return strings.TrimSuffix(bld.String(), "\n")
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func commonPrefix(a, b []string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

func commonSuffix(a, b []string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return i
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
