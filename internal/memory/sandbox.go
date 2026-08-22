package memory

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
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
	_, _ = d.Exec(ctx, id, sandbox.Cmd{
		Argv: []string{"sh", "-c", `if [ -d /workspace/.git ]; then mkdir -p /workspace/.git/info; printf %s "$BLOB" | base64 -d >> /workspace/.git/info/exclude; git update-index --skip-worktree -- AGENTS.md CLAUDE.md .cursor/rules/zeroth-memory.mdc 2>/dev/null || true; fi`},
		Env:  []string{"BLOB=" + b64},
	})
	return nil
}
