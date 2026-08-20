# Claude Code

Read [AGENTS.md](AGENTS.md) first. That file is canonical for layout, commands, and stage-1 constraints.

This repository’s stage-1 harness port is Claude Code (`internal/harness/claudecode`). When you work here:

- You are a *driven* agent, not the kernel. Do not add a shortcut that bypasses plan-then-apply.
- Prefer `go build ./...` and `go test ./...` over introducing a second build system.
- Keep changes inside the package that owns the behavior. Cross-cutting “util” packages are a last resort.
