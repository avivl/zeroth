#!/usr/bin/env bash
# Per-boot startup for Zeroth: bring up the Docker daemon the sandbox driver
# (internal/sandbox/docker) and zerothd's socket probe depend on. There is no
# systemd in the Cloud Agent VM, so dockerd is launched directly. Idempotent:
# a daemon that is already up is left alone.
set -euo pipefail

log() { printf '\n=== %s ===\n' "$1"; }

if sudo docker info >/dev/null 2>&1; then
  log "Docker already running"
  exit 0
fi

log "Starting dockerd"
# Redirect under sudo so the log file is opened as root; a plain
# "sudo dockerd >/var/log/..." would open the file as the unprivileged user.
sudo sh -c 'nohup dockerd >/var/log/dockerd.log 2>&1 &'

for _ in $(seq 1 30); do
  if sudo docker info >/dev/null 2>&1; then
    log "Docker is up"
    exit 0
  fi
  sleep 1
done

echo "dockerd did not become ready in time" >&2
sudo tail -n 40 /var/log/dockerd.log >&2 || true
exit 1
