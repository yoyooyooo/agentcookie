#!/usr/bin/env bash

# Generate the value-free machine-readable manifest published beside release
# archives. checksums.txt must already exist in the asset directory.

set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: scripts/generate-release-manifest.sh <tag> <source-sha> <asset-dir>" >&2
  exit 1
fi

tag="$1"
source_sha="$2"
asset_dir="$3"
checksums="$asset_dir/checksums.txt"
darwin_metadata="$asset_dir/darwin-build.json"
linux_metadata="$asset_dir/linux-build.json"
out="$asset_dir/release-manifest.json"

[[ "$tag" =~ ^fork-v[0-9]+\.[0-9]+\.[0-9]+-r[0-9]+$ ]] || {
  echo "generate-release-manifest.sh: invalid fork release tag: $tag" >&2
  exit 2
}
[[ "$source_sha" =~ ^[0-9a-f]{40}$ ]] || {
  echo "generate-release-manifest.sh: invalid source SHA: $source_sha" >&2
  exit 2
}

for path in "$checksums" "$darwin_metadata" "$linux_metadata"; do
  [[ -f "$path" ]] || {
    echo "generate-release-manifest.sh: missing input: $path" >&2
    exit 2
  }
done

artifacts_json="$({
  while read -r digest filename; do
    [[ -n "$digest" && -n "$filename" ]] || continue
    printf '%s\t%s\n' "$digest" "$filename"
  done < "$checksums"
} | jq -Rn --arg tag "$tag" '
  [inputs
   | split("\t")
   | {sha256: .[0], name: .[1]}
   | . + (.name | capture("^agentcookie_(?<version>.+)_(?<os>darwin|linux)_(?<arch>amd64|arm64)\\.tar\\.gz$"))
   | select(.version == $tag)
   | del(.version)]
')"

artifact_count="$(jq 'length' <<<"$artifacts_json")"
if [[ "$artifact_count" -ne 4 ]]; then
  echo "generate-release-manifest.sh: expected 4 release archives, found $artifact_count" >&2
  jq . <<<"$artifacts_json" >&2
  exit 2
fi

for expected in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
  os="${expected%/*}"
  arch="${expected#*/}"
  count="$(jq --arg os "$os" --arg arch "$arch" '[.[] | select(.os == $os and .arch == $arch)] | length' <<<"$artifacts_json")"
  [[ "$count" -eq 1 ]] || {
    echo "generate-release-manifest.sh: expected exactly one $expected archive" >&2
    exit 2
  }
done

jq -n \
  --arg tag "$tag" \
  --arg source_sha "$source_sha" \
  --arg upstream_tag "v1.1.0" \
  --arg upstream_sha "97dd731250b0d9a340f2d0fa776346d807335d60" \
  --arg module "github.com/mvanhorn/agentcookie" \
  --argjson artifacts "$artifacts_json" \
  --slurpfile darwin "$darwin_metadata" \
  --slurpfile linux "$linux_metadata" \
  '{
    schemaVersion: 1,
    tag: $tag,
    sourceSha: $source_sha,
    upstream: {tag: $upstream_tag, sourceSha: $upstream_sha},
    goModule: $module,
    builds: {darwin: $darwin[0], linux: $linux[0]},
    artifacts: $artifacts
  }' > "$out"

jq -e \
  --arg tag "$tag" \
  --arg source_sha "$source_sha" \
  '.schemaVersion == 1 and .tag == $tag and .sourceSha == $source_sha and (.artifacts | length) == 4' \
  "$out" >/dev/null

echo "generate-release-manifest.sh: wrote $out"
