package sandbox

import (
	"path"
	"strings"
)

// ExcludedFromExport reports whether a workspace-relative path must be
// omitted from a checkpoint. The list is the Z1-113 hard exclusion
// (CLI OAuth stores, .git-credentials, shell history, and token caches)
// plus compiled memory artifacts regenerated at hydration (Z1-118).
// Matching is case-insensitive. A directory prefix excludes the tree.
func ExcludedFromExport(rel string) bool {
	return excludedPath(rel, true)
}

// ExcludedFromImport reports whether a path in an incoming tar must be
// dropped. Credentials stay out (Z1-113). Compiled memory paths are
// allowed so a repo's committed AGENTS.md still hydrates; Hydrate then
// overwrites them with the notebook slice.
func ExcludedFromImport(rel string) bool {
	return excludedPath(rel, false)
}

func excludedPath(rel string, compiledMemory bool) bool {
	rel = normalizeExportPath(rel)
	if rel == "" || rel == "." {
		return false
	}
	base := strings.ToLower(path.Base(rel))
	if _, ok := excludedBasenames[base]; ok {
		return true
	}
	if compiledMemory {
		if _, ok := compiledBasenames[base]; ok {
			return true
		}
	}
	lower := strings.ToLower(rel)
	if compiledMemory && lower == compiledCursorRules {
		return true
	}
	for _, prefix := range excludedPrefixes {
		if lower == prefix || strings.HasPrefix(lower, prefix+"/") {
			return true
		}
	}
	if compiledMemory {
		for _, prefix := range compiledPrefixes {
			if lower == prefix || strings.HasPrefix(lower, prefix+"/") {
				return true
			}
		}
	}
	return false
}

func normalizeExportPath(rel string) string {
	rel = filepathToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	for strings.HasPrefix(rel, "/") {
		rel = strings.TrimPrefix(rel, "/")
	}
	rel = path.Clean(rel)
	if rel == "." || rel == "/" {
		return ""
	}
	return rel
}

// excludedBasenames are credential files anywhere in the workspace.
var excludedBasenames = map[string]struct{}{
	".git-credentials":      {},
	".git-credentials.lock": {},
	".netrc":                {},
	"_netrc":                {},
	".bash_history":         {},
	".zsh_history":          {},
	".ash_history":          {},
	".sh_history":           {},
	".history":              {},
	".python_history":       {},
	".node_repl_history":    {},
	".sqlite_history":       {},
	".npmrc":                {},
	".pypirc":               {},
	".claude.json":          {},
}

// compiledBasenames are Z1-118 hydration artifacts. They are omitted
// from checkpoints and rewritten at the next hydration.
var compiledBasenames = map[string]struct{}{
	"agents.md": {},
	"claude.md": {},
}

const compiledCursorRules = ".cursor/rules/zeroth-memory.mdc"

// excludedPrefixes are CLI OAuth stores and token-cache directories.
var excludedPrefixes = []string{
	".config/gh",
	".config/glab-cli",
	".config/gcloud",
	".config/hub",
	".local/share/gh",
	".cache/gh",
	".aws",
	".azure",
	".docker",
	".claude",
	".codex",
}

var compiledPrefixes = []string{
	".zeroth/compiled-memory",
}
