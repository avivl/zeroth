package memory

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// CompiledAgents is the Claude Code / Cursor harness file written at
	// hydration. It is a build artifact, never the source of truth.
	CompiledAgents = "AGENTS.md"
	// CompiledClaude is the Claude Code native companion file.
	CompiledClaude = "CLAUDE.md"
	// CompiledCursorRules is the Cursor native rule file.
	CompiledCursorRules = ".cursor/rules/zeroth-memory.mdc"
	// CompiledMarkerDir holds a small readme so the overlay is obvious.
	CompiledMarkerDir = ".zeroth/compiled-memory"
)

// CompiledPaths are workspace-relative files Hydrate writes and the
// sandbox must omit from checkpoints.
func CompiledPaths() []string {
	return []string{
		CompiledAgents,
		CompiledClaude,
		CompiledCursorRules,
		CompiledMarkerDir + "/README",
	}
}

const compiledHeader = `# Compiled memory

This file is a build artifact of the Zeroth memory notebook (Z1-118).
It is not the source of truth. Do not commit it.
`

const claudeHeader = `# Claude Code

Read AGENTS.md. That file is compiled from the Zeroth memory notebook
at session hydration (Z1-118). It is a build artifact, not the source
of truth. Do not commit it.
`

const cursorHeader = `---
description: Compiled Zeroth memory notebook (Z1-118)
alwaysApply: true
---

This file is a build artifact of the Zeroth memory notebook.
It is not the source of truth. Do not commit it.
`

const markerReadme = `Compiled memory overlay (Z1-118).

AGENTS.md, CLAUDE.md, and .cursor/rules/zeroth-memory.mdc in this
workspace were written at session hydration from the notebook. They
are excluded from checkpoints and commits. The notebook in the store
is the source of truth.
`

// Compile renders the live slice as AGENTS.md. The bytes are what
// hydration writes; a test compares them to the file on disk.
func Compile(facts []Fact) string {
	return renderFacts(compiledHeader, facts)
}

// CompileAll returns every native harness file for the slice.
func CompileAll(facts []Fact) map[string]string {
	out := map[string]string{
		CompiledAgents:                Compile(facts),
		CompiledClaude:                renderFacts(claudeHeader, facts),
		CompiledCursorRules:           renderFacts(cursorHeader, facts),
		CompiledMarkerDir + "/README": markerReadme,
	}
	return out
}

func renderFacts(header string, facts []Fact) string {
	live := make([]Fact, 0, len(facts))
	for _, f := range facts {
		if f.Deleted {
			continue
		}
		live = append(live, f)
	}
	sortFacts(live)
	var bld strings.Builder
	bld.WriteString(strings.TrimRight(header, "\n"))
	bld.WriteByte('\n')
	if len(live) == 0 {
		return bld.String()
	}
	bld.WriteByte('\n')
	for _, f := range live {
		fmt.Fprintf(&bld, "## `%s`\n\n", f.Key)
		bld.WriteString(strings.TrimRight(f.Body, "\n"))
		bld.WriteString("\n\n")
		fmt.Fprintf(&bld, "<!-- provenance who=%s kind=%s at=%s source=%s -->\n\n",
			f.Provenance.Who.Name, f.Provenance.Who.Kind, rfc3339(f.Provenance.When), f.Provenance.Source)
	}
	return strings.TrimRight(bld.String(), "\n") + "\n"
}

// KeysInCompile returns the fact keys present in compiled AGENTS.md,
// used by tests that a delete disappears from the next compilation.
func KeysInCompile(compiled string) []string {
	var keys []string
	for _, line := range strings.Split(compiled, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## `") && strings.HasSuffix(line, "`") {
			keys = append(keys, strings.TrimSuffix(strings.TrimPrefix(line, "## `"), "`"))
		}
	}
	sort.Strings(keys)
	return keys
}
