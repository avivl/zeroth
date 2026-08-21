package sandbox

import (
	"path"
	"strings"
)

// ExcludedFromExport reports whether a workspace-relative path must be
// omitted from a checkpoint. The list is the Z1-113 hard exclusion:
// CLI OAuth stores, .git-credentials, shell history, and token caches.
// Matching is case-insensitive. A directory prefix excludes the tree.
func ExcludedFromExport(rel string) bool {
	rel = normalizeExportPath(rel)
	if rel == "" || rel == "." {
		return false
	}
	base := strings.ToLower(path.Base(rel))
	if excludedBasenames[base] {
		return true
	}
	lower := strings.ToLower(rel)
	for _, prefix := range excludedPrefixes {
		if lower == prefix || strings.HasPrefix(lower, prefix+"/") {
			return true
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
