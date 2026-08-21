#!/usr/bin/env bash
# Build the three spike fixture tars. Uncompressed on purpose: later
# gates measure compression, and synthetic zeros compress unlike a real
# module cache.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
fix="$(cd "$(dirname "$0")" && pwd)"
work="${fix}/work"
mkdir -p "${work}"
# Nested module so `go test ./...` in zeroth-spike does not load fixture
# sources as packages of this module.
cat > "${work}/go.mod" <<'EOF'
module zeroth-spike-fixture-work

go 1.27
EOF

S_TARGET=$((10 * 1024 * 1024))
M_TARGET=$((500 * 1024 * 1024))
L_TARGET=$((5 * 1024 * 1024 * 1024))

writable_rm() {
  if [ -e "$1" ]; then
    chmod -R u+w "$1" 2>/dev/null || true
    rm -rf "$1"
  fi
}

echo "generating S (~10 MB scripts)"
writable_rm "${work}/S"
mkdir -p "${work}/S"
(cd "${root}" && go run ./cmd/genfixtures -kind S -out "${work}/S" -bytes "${S_TARGET}")
tar -C "${work}/S" -cf "${fix}/S.tar" .
echo "S.tar $(wc -c < "${fix}/S.tar") bytes (unpacked $(du -sb "${work}/S" | cut -f1))"

echo "generating M (~500 MB real Go repo + module cache)"
stage="${work}/.mstage"
mkdir -p "${stage}"
if [ ! -d "${stage}/prometheus/.git" ]; then
  writable_rm "${stage}/prometheus"
  git clone --depth 1 --single-branch https://github.com/prometheus/prometheus.git "${stage}/prometheus"
fi
mkdir -p "${stage}/mod"
(
  cd "${stage}/prometheus"
  GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
    GOSUMDB="${GOSUMDB:-sum.golang.org}" \
    GOMODCACHE="${stage}/mod" \
    GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.0}" \
    go mod download
)

writable_rm "${work}/M"
mkdir -p "${work}/M/prometheus" "${work}/M/gopath/pkg/mod"
cp -a "${stage}/prometheus/." "${work}/M/prometheus/"
repo_bytes="$(du -sb "${work}/M" | cut -f1)"
fill=$((M_TARGET - repo_bytes))
if [ "${fill}" -lt $((100 * 1024 * 1024)) ]; then
  fill=$((400 * 1024 * 1024))
fi
# Copy real extracted modules (not the download zip cache, not a toolchain)
# until the tree is about 500 MB. These are genuine dependency sources.
(cd "${root}" && go run ./cmd/genfixtures -kind Mfill -src "${stage}/mod" -out "${work}/M/gopath/pkg/mod" -bytes "${fill}")
tar -C "${work}/M" -cf "${fix}/M.tar" .
m_unpacked="$(du -sb "${work}/M" | cut -f1)"
m_tar="$(wc -c < "${fix}/M.tar")"
echo "M.tar ${m_tar} bytes (unpacked ${m_unpacked})"

echo "generating L (~5 GB repo plus binary assets)"
writable_rm "${work}/L"
mkdir -p "${work}/L"
cp -a "${work}/M/." "${work}/L/"
l_now="$(du -sb "${work}/L" | cut -f1)"
pad=$((L_TARGET - l_now))
if [ "${pad}" -lt $((1024 * 1024 * 1024)) ]; then
  pad=$((4 * 1024 * 1024 * 1024))
fi
(cd "${root}" && go run ./cmd/genfixtures -kind Lpad -out "${work}/L" -bytes "${pad}")
tar -C "${work}/L" -cf "${fix}/L.tar" .
l_unpacked="$(du -sb "${work}/L" | cut -f1)"
l_tar="$(wc -c < "${fix}/L.tar")"
echo "L.tar ${l_tar} bytes (unpacked ${l_unpacked})"

s_unpacked="$(du -sb "${work}/S" | cut -f1)"
s_tar="$(wc -c < "${fix}/S.tar")"

manifest="${fix}/MANIFEST.md"
cat > "${manifest}" <<EOF
# Fixture sizes

Recorded by \`fixtures/build.sh\`. Tars are uncompressed (\`tar -cf\`) so later
gates can measure compression. Fixture M is a real Go clone plus its module
cache, not synthetic files.

GitHub rejects blobs over 100 MB and Git LFS caps a file at 2 GB, so only
**S.tar** is committed. Recreate M and L locally with \`./fixtures/build.sh\`.

| Size | Tar | Tar bytes | Unpacked bytes | Contents |
| --- | --- | ---: | ---: | --- |
| S | \`zeroth-spike/fixtures/S.tar\` | ${s_tar} | ${s_unpacked} | scripts |
| M | \`zeroth-spike/fixtures/M.tar\` | ${m_tar} | ${m_unpacked} | prometheus clone + GOMODCACHE |
| L | \`zeroth-spike/fixtures/L.tar\` | ${l_tar} | ${l_unpacked} | M plus binary assets |
EOF

echo "wrote ${manifest}"
