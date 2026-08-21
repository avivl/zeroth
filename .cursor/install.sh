#!/usr/bin/env bash
# Idempotent Cloud Agent bootstrap for Zeroth.
#
# Runs after checkout. Installs the two toolchain pieces the default image
# lacks (Task and Docker), primes the Go build cache, and installs the web
# pnpm workspace. Safe to run repeatedly and safe on a machine that has none
# of this yet, so it works both for a fresh default image and when re-run
# against a prepared snapshot.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

log() { printf '\n=== %s ===\n' "$1"; }

TASK_VERSION="v3.40.0"

# Task and Docker are not in the default image. modernc.org/sqlite keeps the
# Go build cgo-free, so only Docker (sandbox driver) and Task are needed on
# top of the image's Go, Node, pnpm, git, and curl.
need_apt=0
command -v docker >/dev/null 2>&1 || need_apt=1
command -v fuse-overlayfs >/dev/null 2>&1 || need_apt=1
if [ "${need_apt}" = "1" ]; then
  log "Installing system packages (Docker + fuse-overlayfs)"
  export DEBIAN_FRONTEND=noninteractive
  sudo apt-get update -qq
  # fuse.conf ships a conffile prompt; keep the maintainer version non-interactively.
  sudo apt-get install -y -qq --no-install-recommends \
    -o Dpkg::Options::=--force-confold \
    docker.io docker-compose-v2 fuse-overlayfs uidmap iptables ca-certificates curl
fi

if ! command -v task >/dev/null 2>&1; then
  log "Installing Task ${TASK_VERSION}"
  sudo sh -c "curl -fsSL https://taskfile.dev/install.sh | sh -s -- -d -b /usr/local/bin ${TASK_VERSION}"
fi

# The default overlay2/containerd snapshotter cannot mount overlay inside the
# nested Cloud Agent VM. The classic fuse-overlayfs graph driver works.
log "Configuring Docker daemon for the nested VM"
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json >/dev/null <<'JSON'
{
  "storage-driver": "fuse-overlayfs",
  "features": { "containerd-snapshotter": false }
}
JSON

# Reach the Docker socket without sudo from the unprivileged user.
sudo groupadd -f docker
sudo usermod -aG docker "$(id -un)"

# Prime the module cache and prove every package compiles. GOTOOLCHAIN pins
# the same Go the Taskfile and CI use.
log "Building Go packages"
export GOTOOLCHAIN=go1.27.0
go build ./...

log "Installing web dependencies"
export COREPACK_ENABLE_DOWNLOAD_PROMPT=0
corepack pnpm install --frozen-lockfile

log "install complete"
