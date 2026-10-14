#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s <preset>\n\nPresets:\n  cli  Build the Reasonix CLI (includes the interactive TUI)\n' "$0"
}

if [[ $# -ne 1 ]]; then
  usage >&2
  exit 2
fi

case "$1" in
  cli)
    repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
    exec make -C "$repo_root" build-cli
    ;;
  -h|--help)
    usage
    ;;
  *)
    printf 'Unknown preset: %s\n' "$1" >&2
    usage >&2
    exit 2
    ;;
esac
