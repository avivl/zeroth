# web

Vite + React UI for the local daemon.

Stage 1 is single-player and talks to `zerothd` on the same machine (`127.0.0.1:8420` by default). [Beautiful UI](https://www.beautifului.dev/) (MIT) is the intended primitive kit; it is not wired up in this skeleton.

The TypeScript client under `pkg/api/gen/ts` is generated from the OpenAPI spec and is HTTP-only. Live run events use the hand-written WebSocket helper in `src/api/runEvents.ts`, typed against the generated `RunEvent` schema. Do not treat that helper as a second contract.

```bash
task web
```

Or, from this directory, `pnpm install` and `pnpm run dev`. Lint is `pnpm run lint` (`tsc -b`), also run by `task lint` from the repo root. Tests are `pnpm run test` (vitest).
