package memory

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/avivl/zeroth/internal/sandbox"
)

// HydrateSandbox writes the compiled slice into a live sandbox overlay
// through Exec. Files land under /workspace. Checkpoints omit them via
// ExcludedFromExport; commits are blocked by .git/info/exclude when a
// repo is present.
func HydrateSandbox(ctx context.Context, d sandbox.Driver, id sandbox.ID, facts []Fact) error {
	if d == nil {
		return fmt.Errorf("memory hydrate sandbox: nil driver: %w", ErrInvalid)
	}
	files := CompileAll(facts)
	for rel, body := range files {
		dest := path.Join(sandbox.WorkspaceDir, rel)
		b64 := base64.StdEncoding.EncodeToString([]byte(body))
		res, err := d.Exec(ctx, id, sandbox.Cmd{
			Argv: []string{"sh", "-c", `mkdir -p "$(dirname "$DEST")" && printf %s "$BLOB" | base64 -d > "$DEST"`},
			Env:  []string{"DEST=" + dest, "BLOB=" + b64},
		})
		if err != nil {
			return fmt.Errorf("memory hydrate sandbox %s: %w", rel, err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("memory hydrate sandbox %s: exit %d: %s", rel, res.ExitCode, res.Stderr)
		}
	}
	excludeBody := excludeMarker + "\n" + strings.Join(CompiledPaths(), "\n") + "\n" + CompiledMarkerDir + "/\n"
	b64 := base64.StdEncoding.EncodeToString([]byte(excludeBody))
	res, err := d.Exec(ctx, id, sandbox.Cmd{
		Argv: []string{"sh", "-c", protectSandboxScript()},
		Env:  []string{"BLOB=" + b64},
	})
	if err != nil {
		return fmt.Errorf("memory hydrate sandbox protect: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("memory hydrate sandbox protect: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return nil
}

// protectSandboxScript writes .git/info/exclude and sets skip-worktree
// on tracked compiled paths. set -e so a failed mkdir, exclude write, or
// skip-worktree is the script's exit status rather than || true.
// Untracked paths are covered by exclude; ls-files failing in the if
// test is not a protection failure. Missing git is the same: alpine
// images have no git binary, and seedOverlay does not copy .git.
func protectSandboxScript() string {
	var b strings.Builder
	b.WriteString(`set -e
if [ ! -d /workspace/.git ]; then
  exit 0
fi
mkdir -p /workspace/.git/info
printf %s "$BLOB" | base64 -d >> /workspace/.git/info/exclude
`)
	for _, p := range CompiledPaths() {
		q := strconv.Quote(p)
		fmt.Fprintf(&b, "if git ls-files --error-unmatch -- %s >/dev/null 2>&1; then git update-index --skip-worktree -- %s; fi\n", q, q)
	}
	return b.String()
}
