package server

import (
	"fmt"
	"strconv"
	"strings"
)

func isUnifiedDiff(s string) bool {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if strings.HasPrefix(s, "diff --git ") || strings.Contains(s, "\ndiff --git ") {
		return true
	}
	if strings.HasPrefix(s, "@@ ") || strings.Contains(s, "\n@@ ") {
		return true
	}
	return false
}

func applyUnifiedDiff(original []byte, diff string) ([]byte, error) {
	hunks, err := parseUnifiedHunks(diff)
	if err != nil {
		return nil, err
	}
	if len(hunks) == 0 {
		return nil, fmt.Errorf("unified diff has no hunks")
	}
	src := splitLines(string(original))
	var out []string
	cursor := 0
	for i, h := range hunks {
		start := h.oldStart
		if start > 0 {
			start--
		}
		if start < cursor || start > len(src) {
			return nil, fmt.Errorf("hunk %d: old start %d is out of range", i+1, h.oldStart)
		}
		out = append(out, src[cursor:start]...)
		cursor = start
		for _, line := range h.lines {
			switch line.kind {
			case hunkContext, hunkDelete:
				if cursor >= len(src) || src[cursor] != line.text {
					got := "<eof>"
					if cursor < len(src) {
						got = src[cursor]
					}
					return nil, fmt.Errorf("hunk %d: conflict, expected %q, got %q", i+1, line.text, got)
				}
				if line.kind == hunkContext {
					out = append(out, src[cursor])
				}
				cursor++
			case hunkAdd:
				out = append(out, line.text)
			}
		}
	}
	out = append(out, src[cursor:]...)
	return joinLines(out, original), nil
}

type hunkKind int

const (
	hunkContext hunkKind = iota
	hunkAdd
	hunkDelete
)

type hunkLine struct {
	kind hunkKind
	text string
}

type unifiedHunk struct {
	oldStart int
	lines    []hunkLine
}

func parseUnifiedHunks(diff string) ([]unifiedHunk, error) {
	diff = strings.ReplaceAll(diff, "\r\n", "\n")
	raw := strings.Split(diff, "\n")
	var hunks []unifiedHunk
	var cur *unifiedHunk
	for _, line := range raw {
		if strings.HasPrefix(line, "@@ ") {
			oldStart, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, unifiedHunk{oldStart: oldStart})
			cur = &hunks[len(hunks)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if line == "\\ No newline at end of file" {
			continue
		}
		if line == "" {
			continue
		}
		switch line[0] {
		case ' ':
			cur.lines = append(cur.lines, hunkLine{kind: hunkContext, text: line[1:]})
		case '+':
			cur.lines = append(cur.lines, hunkLine{kind: hunkAdd, text: line[1:]})
		case '-':
			cur.lines = append(cur.lines, hunkLine{kind: hunkDelete, text: line[1:]})
		default:
			return nil, fmt.Errorf("unified diff: unexpected line %q", line)
		}
	}
	return hunks, nil
}

func parseHunkHeader(line string) (int, error) {
	// @@ -oldStart,oldCount +newStart,newCount @@
	rest := strings.TrimPrefix(line, "@@ ")
	parts := strings.Split(rest, " ")
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "-") {
		return 0, fmt.Errorf("unified diff: bad hunk header %q", line)
	}
	spec := strings.TrimPrefix(parts[0], "-")
	start, _, _ := strings.Cut(spec, ",")
	n, err := strconv.Atoi(start)
	if err != nil {
		return 0, fmt.Errorf("unified diff: bad hunk header %q", line)
	}
	return n, nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

func joinLines(lines []string, original []byte) []byte {
	if len(lines) == 0 {
		return []byte{}
	}
	body := strings.Join(lines, "\n")
	if len(original) == 0 || strings.HasSuffix(string(original), "\n") || strings.HasSuffix(string(original), "\r\n") {
		body += "\n"
	}
	return []byte(body)
}
