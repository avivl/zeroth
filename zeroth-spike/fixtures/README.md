# Fixture workspaces

Three uncompressed tars used by the BA-6 confirmation spike. Compression
is a later gate, so these are `tar -cf`, not gzip.

| Size | Target | What it is |
| --- | --- | --- |
| **S** | ~10 MB | scripts |
| **M** | ~500 MB | a real Go repo clone with a populated module cache |
| **L** | ~5 GB | that repo plus binary assets |

Fixture M must be a genuine dependency tree. Synthetic files compress unlike
a real `GOMODCACHE`.

## Build

```bash
./fixtures/build.sh
```

Only **S.tar** is in git. M (~500 MB) and L (~5 GB) exceed GitHub's blob
limit (100 MB) and L exceeds Git LFS (2 GB). Sizes for all three are
recorded in [MANIFEST.md](MANIFEST.md) and copied into
[RESULTS.md](../../docs/spike/RESULTS.md).

`build.sh` clones [prometheus/prometheus](https://github.com/prometheus/prometheus)
and runs `go mod download` into an in-tree module cache. If that tree is
still under 400 MB it also clones [hashicorp/terraform](https://github.com/hashicorp/terraform).
L copies M and pads with a SHA-256 stream of binary assets.
