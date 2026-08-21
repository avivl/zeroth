# Spike results (BA-6)

Confirmation spike. Fill a **Result** cell when that gate is run. Empty
means not measured. This file is not an ADR. Promote a decision into
[`docs/adr/`](../adr/) when the gate closes.

Setup (Linear [42-5](https://linear.app/42-golems/issue/42-5/spike-setup-throwaway-repo-fixtures-resultsmd-template)): throwaway
tree in [`zeroth-spike/`](../../zeroth-spike/), compose stack, fixture tars,
Anthropic API key from the environment ([ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md)).

## Fixtures

Uncompressed tars. Fixture M is a real Go clone plus its module cache, not
synthetic files. Only S.tar is in git. Recreate M and L with
`zeroth-spike/fixtures/build.sh`. Live numbers: [`zeroth-spike/fixtures/MANIFEST.md`](../../zeroth-spike/fixtures/MANIFEST.md).

| Size | Tar | Tar bytes | Unpacked bytes | Contents |
| --- | --- | ---: | ---: | --- |
| S | `zeroth-spike/fixtures/S.tar` | 10516480 | 10485884 | scripts (~10 MB) |
| M | `zeroth-spike/fixtures/M.tar` | 557199360 | 524291722 | real Go repo + module cache (~500 MB) |
| L | `zeroth-spike/fixtures/L.tar` | 5401610240 | 5368709120 | M plus binary assets (~5 GB) |

## Gates

| Gate | Linear | Question | Pass bar | Result | Evidence |
| --- | --- | --- | --- | --- | --- |
| G1 | | Docker sandbox `Driver`: start, exec, stop against a real container. Isolation from the host. | Interface holds. Host writes from inside the sandbox fail. | | |
| G2 | | Workspace ingest of fixture **S** (~10 MB scripts): copy, compress, unpack. | Times recorded. No data loss. | | |
| G3 | | Workspace ingest of fixture **M** (~500 MB, genuine module cache). Compression vs S. | Times and ratios recorded. Real deps, not synthetic files. | | |
| G4 | | Workspace ingest of fixture **L** (~5 GB, binary assets). Compression vs M. | Times and ratios recorded. Binaries stay large. | | |
| G5 | | Session state machine and append-only event log. Distinct `session.ID`. | Illegal transitions deny. Log is the source of truth. | | |
| G6 | | Harness touchpoint with Anthropic API key only ([ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md)). | Key from env. No consumer OAuth. Key never logged. | | |
| G7 | [42-9](https://linear.app/42-golems/issue/42-9/gate-g7-evaluate-acp-as-the-harness-driver-protocol-write-adr-z-0003) | Evaluate ACP as the harness driver protocol. Write [ADR-Z-0003](../adr/Z-0003-harness-driver-protocol.md). | ADR accepted with ACP or a shim. Plan-then-apply still holds. | | |
