# Claude Code

Read [AGENTS.md](AGENTS.md). That file is canonical for layout, kernel rules, ports, tests, style, and PRs. Do not copy those rules here.

This repository's stage-1 harness port is Claude Code (`internal/harness/claudecode`). When you work in this repo you are a driven agent, not the kernel.

The standard stack is Zap, Cobra+Viper, and Failsafe-go. It is named in [AGENTS.md](AGENTS.md). Do not introduce a second logging, CLI, config, or resilience approach. `internal/policy` stays I/O-free: inject a logger, never a global.
