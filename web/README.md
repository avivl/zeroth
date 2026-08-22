# web

Vite + React UI for the local daemon.

Stage 1 is single-player and talks to `zerothd` on the same machine (`127.0.0.1:8420` by default). The seven operator views (runs, run detail, agents, agent configuration, approvals, project memory, audit) call the generated TypeScript client from `pkg/api`. Live run events use the hand-written WebSocket helper in `src/api/runEvents.ts`, typed against the generated `RunEvent` schema. That helper reconnects after a drop; it is not a second contract.

The visual language follows [Beautiful UI](https://www.beautifului.dev/) (MIT): Approval Card, Diff Table, Task Rows, Thinking, Streaming Text. Light and dark themes live on `[data-theme]`. Space Grotesk is display, Inter is UI, JetBrains Mono is for IDs, leases, diffs, and signatures. Signature chips stay dark in both themes.

`task web` proxies API paths and the run-events WebSocket to `127.0.0.1:8420`.

```bash
task web
```

Or, from this directory, `pnpm install` and `pnpm run dev`. Lint is `pnpm run lint` (`tsc -b`), also run by `task lint` from the repo root. Tests are `pnpm run test` (vitest).
