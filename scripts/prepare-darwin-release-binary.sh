#!/usr/bin/env bash

# Sign one Darwin release binary and notarize it when a fork-owned Developer
# ID authority is configured. The selected mode is recorded separately by the
# release workflow; this script never silently downgrades a requested mode.

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: scripts/prepare-darwin-release-binary.sh <binary>" >&2
  exit 1
fi

binary="$1"
mode="${AGENTCOOKIE_DARWIN_RELEASE_MODE:-adhoc}"

case "$mode" in
  developer-id)
    AGENTCOOKIE_SIGN_MODE=developer-id scripts/sign.sh "$binary"
    scripts/notarize.sh "$binary"
    ;;
  adhoc)
    AGENTCOOKIE_SIGN_MODE=adhoc scripts/sign.sh "$binary"
    ;;
  *)
    echo "prepare-darwin-release-binary.sh: unsupported mode: $mode" >&2
    exit 1
    ;;
esac
