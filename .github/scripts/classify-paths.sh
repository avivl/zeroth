#!/usr/bin/env bash
# Classify changed paths for CI.
#
# web/-only PRs (and root pnpm workspace files) skip Go.
# internal/-only PRs skip web.
# pkg/api/ is the contract: always run both.
# Anything else (cmd/, docs/, .github/, Taskfile) runs both.
#
# Usage:
#   classify-paths.sh --all
#   classify-paths.sh <base-sha> <head-sha>
#   CLASSIFY_FILES=$'web/a.tsx\ninternal/x.go' classify-paths.sh
set -euo pipefail

emit() {
  local go="$1" web="$2"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    {
      echo "go=${go}"
      echo "web=${web}"
    } >> "${GITHUB_OUTPUT}"
  fi
  echo "go=${go}"
  echo "web=${web}"
}

if [ "${1:-}" = "--all" ]; then
  emit true true
  exit 0
fi

files=()
if [ -n "${CLASSIFY_FILES:-}" ]; then
  while IFS= read -r line; do
    [ -n "$line" ] && files+=("$line")
  done <<< "${CLASSIFY_FILES}"
elif [ "$#" -eq 2 ]; then
  while IFS= read -r line; do
    [ -n "$line" ] && files+=("$line")
  done < <(git diff --name-only "$1" "$2")
else
  echo "usage: $0 --all | $0 <base-sha> <head-sha>" >&2
  echo "or set CLASSIFY_FILES to a newline-separated path list" >&2
  exit 2
fi

if [ "${#files[@]}" -eq 0 ]; then
  emit true true
  exit 0
fi

echo "changed paths:" >&2
printf '  %s\n' "${files[@]}" >&2

all_web=1
all_internal=1
has_api=0

for f in "${files[@]}"; do
  case "$f" in
    pkg/api | pkg/api/*) has_api=1 ;;
  esac
  case "$f" in
    web | web/* | package.json | pnpm-lock.yaml | pnpm-workspace.yaml) ;;
    *) all_web=0 ;;
  esac
  case "$f" in
    internal | internal/*) ;;
    *) all_internal=0 ;;
  esac
done

if [ "$has_api" -eq 1 ]; then
  emit true true
  exit 0
fi

go=true
web=true
if [ "$all_web" -eq 1 ]; then
  go=false
fi
if [ "$all_internal" -eq 1 ]; then
  web=false
fi

emit "$go" "$web"
