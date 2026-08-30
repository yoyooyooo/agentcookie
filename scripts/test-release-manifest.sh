#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fixture="$(mktemp -d -t agentcookie-release-manifest.XXXXXX)"
trap 'rm -rf "$fixture"' EXIT

tag="fork-v1.1.0-r4"
source_sha="0123456789abcdef0123456789abcdef01234567"

for target in darwin_arm64 darwin_amd64 linux_amd64 linux_arm64; do
  printf '%s\n' "$target" > "$fixture/agentcookie_${tag}_${target}.tar.gz"
done

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$fixture" && sha256sum ./*.tar.gz | sed 's#  \./#  #' > checksums.txt)
else
  (cd "$fixture" && shasum -a 256 ./*.tar.gz | sed 's#  \./#  #' > checksums.txt)
fi

jq -n '{platform:"darwin",version:"fork-v1.1.0-r4",signingMode:"adhoc",notarized:false,builder:"test"}' > "$fixture/darwin-build.json"
jq -n '{platform:"linux",version:"fork-v1.1.0-r4",signingMode:"unsigned",notarized:false,builder:"test"}' > "$fixture/linux-build.json"

"$repo_root/scripts/generate-release-manifest.sh" "$tag" "$source_sha" "$fixture"

jq -e \
  --arg tag "$tag" \
  --arg source_sha "$source_sha" \
  '.tag == $tag and .sourceSha == $source_sha and (.artifacts | length) == 4 and .builds.darwin.signingMode == "adhoc"' \
  "$fixture/release-manifest.json" >/dev/null

if "$repo_root/scripts/generate-release-manifest.sh" "v1.1.0" "$source_sha" "$fixture" >/dev/null 2>&1; then
  echo "test-release-manifest.sh: invalid tag unexpectedly passed" >&2
  exit 1
fi

echo "test-release-manifest.sh: pass"
