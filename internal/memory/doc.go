// Package memory is the notebook of atomic facts agents and humans share.
//
// Humans write directly. Agents cannot. Agent writes land in a propose
// queue and enter the notebook only on human accept. That rule is not
// configurable: memory.propose cannot be turned off or made direct
// (Z1-022). Anything an agent can recall, a human in that scope can
// open, correct, or delete.
//
// Stage 1 is store-backed. Persistence is [store.Store]; this package
// owns the propose-first rules, version history, and compilation.
// At session hydration the live slice compiles into the harness's native
// files inside the sandbox (AGENTS.md, CLAUDE.md, .cursor/rules). Those
// files are a build artifact of the notebook, never the source of truth,
// and they are excluded from checkpoints and commits (Z1-118).
package memory
