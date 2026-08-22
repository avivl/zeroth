package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// shrinkFloor is the minimum original size (bytes) at which a modify
// that is not a deletion patch is refused for shrinking the file. Tiny
// files may still be rewritten in full.
const shrinkFloor = 256

// Digest is SHA-256 of body, hex-encoded. Precondition and postcondition
// hashes use this so draft and apply compare the same function.
func Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// EmptyDigest is the postcondition of a destroy: the hash of absence.
func EmptyDigest() string {
	return Digest(nil)
}

// Materialize turns a row payload into the bytes that should land on
// disk. Create writes the payload as the new file. Modify patches the
// original; a diff-shaped payload is never treated as the whole file.
// Destroy returns a nil body.
func Materialize(op Op, original []byte, payload string) ([]byte, error) {
	switch op {
	case OpCreate:
		return []byte(payload), nil
	case OpModify:
		return applyModify(original, payload)
	case OpDestroy:
		return nil, nil
	case OpMemoryProposal:
		return []byte(payload), nil
	default:
		return nil, fmt.Errorf("unsupported op %s", op)
	}
}

func applyModify(original []byte, payload string) ([]byte, error) {
	if LooksLikeDiff(payload) {
		out, err := ApplyPatch(original, payload)
		if err != nil {
			return nil, fmt.Errorf("modify payload is a diff but does not apply: %w", err)
		}
		if wouldShrinkDrastically(original, out) && !patchDeletesContent(payload) {
			return nil, fmt.Errorf("modify payload would shrink the file from %d to %d bytes without deletion hunks", len(original), len(out))
		}
		return out, nil
	}
	if wouldShrinkDrastically(original, []byte(payload)) {
		return nil, fmt.Errorf("modify payload would replace %d bytes with %d bytes; send a unified diff with context instead of overwriting", len(original), len(payload))
	}
	return []byte(payload), nil
}

func wouldShrinkDrastically(old, neu []byte) bool {
	if len(old) < shrinkFloor {
		return false
	}
	return len(neu)*2 < len(old)
}

func patchDeletesContent(payload string) bool {
	for _, line := range strings.Split(normalizeNL(payload), "\n") {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			return true
		}
	}
	return false
}

// LooksLikeDiff reports whether s is a unified diff, a hunk header, or a
// raw +/- hunk. Full file contents (no diff markers) return false.
func LooksLikeDiff(s string) bool {
	s = normalizeNL(s)
	if strings.HasPrefix(s, "diff --git ") || strings.Contains(s, "\ndiff --git ") {
		return true
	}
	hasMinusMinus, hasPlusPlus := false, false
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "@@") {
			return true
		}
		if strings.HasPrefix(line, "--- ") {
			hasMinusMinus = true
		}
		if strings.HasPrefix(line, "+++ ") {
			hasPlusPlus = true
		}
	}
	if hasMinusMinus && hasPlusPlus {
		return true
	}
	return isRawHunk(s)
}

func isRawHunk(s string) bool {
	plusMinus := 0
	for _, line := range strings.Split(strings.TrimSpace(normalizeNL(s)), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case '+', '-':
			plusMinus++
		case ' ':
			// context in a raw hunk
		default:
			return false
		}
	}
	return plusMinus > 0
}

// ApplyPatch applies a unified diff or a raw +/- hunk to original.
func ApplyPatch(original []byte, diff string) ([]byte, error) {
	hunks, err := parseUnifiedHunks(diff)
	if err != nil {
		return nil, err
	}
	if len(hunks) == 0 {
		hunks, err = parseRawHunks(diff)
		if err != nil {
			return nil, err
		}
	}
	if len(hunks) == 0 {
		return nil, fmt.Errorf("unified diff has no hunks")
	}
	return applyHunks(original, hunks)
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
	oldStart    int
	unspecified bool
	lines       []hunkLine
}

func parseUnifiedHunks(diff string) ([]unifiedHunk, error) {
	raw := strings.Split(normalizeNL(diff), "\n")
	var hunks []unifiedHunk
	var cur *unifiedHunk
	for _, line := range raw {
		if strings.HasPrefix(line, "@@") {
			oldStart, unspecified, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, unifiedHunk{oldStart: oldStart, unspecified: unspecified})
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

func parseRawHunks(diff string) ([]unifiedHunk, error) {
	var lines []hunkLine
	for _, line := range strings.Split(normalizeNL(diff), "\n") {
		if line == "" || line == "\\ No newline at end of file" {
			continue
		}
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			continue
		}
		switch line[0] {
		case ' ':
			lines = append(lines, hunkLine{kind: hunkContext, text: line[1:]})
		case '+':
			lines = append(lines, hunkLine{kind: hunkAdd, text: line[1:]})
		case '-':
			lines = append(lines, hunkLine{kind: hunkDelete, text: line[1:]})
		default:
			return nil, fmt.Errorf("unified diff: unexpected line %q", line)
		}
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return []unifiedHunk{{unspecified: true, lines: lines}}, nil
}

func parseHunkHeader(line string) (int, bool, error) {
	trimmed := strings.TrimSpace(line)
	rest := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "@@"), "@@"))
	if rest == "" {
		return 0, true, nil
	}
	parts := strings.Fields(rest)
	if len(parts) < 1 || !strings.HasPrefix(parts[0], "-") {
		return 0, false, fmt.Errorf("unified diff: bad hunk header %q", line)
	}
	spec := strings.TrimPrefix(parts[0], "-")
	start, _, _ := strings.Cut(spec, ",")
	n, err := strconv.Atoi(start)
	if err != nil {
		return 0, false, fmt.Errorf("unified diff: bad hunk header %q", line)
	}
	return n, false, nil
}

func applyHunks(original []byte, hunks []unifiedHunk) ([]byte, error) {
	src := splitLines(string(original))
	var out []string
	cursor := 0
	for i, h := range hunks {
		old := oldTexts(h)
		neu := newTexts(h)
		if len(old) == 0 {
			if !h.unspecified && h.oldStart > 0 {
				start := h.oldStart - 1
				if start < cursor || start > len(src) {
					return nil, fmt.Errorf("hunk %d: insert position %d is out of range", i+1, h.oldStart)
				}
				out = append(out, src[cursor:start]...)
				out = append(out, neu...)
				cursor = start
				continue
			}
			out = append(out, src[cursor:]...)
			cursor = len(src)
			out = append(out, neu...)
			continue
		}
		start := -1
		if !h.unspecified && h.oldStart > 0 {
			cand := h.oldStart - 1
			if cand >= cursor && matchAt(src, cand, old) {
				start = cand
			}
		}
		if start < 0 {
			start = indexOf(src, old, cursor)
		}
		if start < 0 {
			got := "<eof>"
			if cursor < len(src) {
				got = src[cursor]
			}
			want := old[0]
			return nil, fmt.Errorf("hunk %d: conflict, expected %q, got %q", i+1, want, got)
		}
		out = append(out, src[cursor:start]...)
		out = append(out, neu...)
		cursor = start + len(old)
	}
	out = append(out, src[cursor:]...)
	return joinLines(out, original), nil
}

func oldTexts(h unifiedHunk) []string {
	var out []string
	for _, line := range h.lines {
		if line.kind == hunkContext || line.kind == hunkDelete {
			out = append(out, line.text)
		}
	}
	return out
}

func newTexts(h unifiedHunk) []string {
	var out []string
	for _, line := range h.lines {
		if line.kind == hunkContext || line.kind == hunkAdd {
			out = append(out, line.text)
		}
	}
	return out
}

func matchAt(src []string, at int, old []string) bool {
	if at < 0 || at+len(old) > len(src) {
		return false
	}
	for i, line := range old {
		if src[at+i] != line {
			return false
		}
	}
	return true
}

func indexOf(src []string, old []string, from int) int {
	if len(old) == 0 {
		return from
	}
	for i := from; i+len(old) <= len(src); i++ {
		if matchAt(src, i, old) {
			return i
		}
	}
	return -1
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = normalizeNL(s)
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
	orig := string(original)
	if len(original) == 0 || strings.HasSuffix(orig, "\n") || strings.HasSuffix(orig, "\r\n") {
		body += "\n"
	}
	return []byte(body)
}

func normalizeNL(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
