# Fork Release Runbook

## Preconditions

A release candidate must have:

- an accepted `fork/vX.Y.Z` generation based on the exact official tag;
- no merge commits in the fork-owned range;
- a clean `git diff --check`;
- local checks and required GitHub Actions green for the exact SHA;
- a generation manifest with every previous delta classified;
- no unresolved `blocked` item inside the claimed release scope.

Source acceptance does not imply an artifact exists or is deployed.

## Tag Namespace

Use an immutable, annotated fork-specific tag:

```bash
fork-v1.1.0-r4
```

Before creating it, show and record the exact candidate:

```bash
git rev-parse fork/v1.1.0
git log -1 --oneline fork/v1.1.0
git rev-list --merges v1.1.0..fork/v1.1.0
git diff --check v1.1.0..fork/v1.1.0
```

Never use or move the official `v1.1.0` namespace. A correction receives the
next `rN`; every prior tag remains immutable.

## CI Release Path

A pushed `fork-v*` tag starts the release workflow. The prepare job rejects a
tag unless it matches `fork-vX.Y.Z-rN`, is annotated, and peels to the exact
workflow SHA.

GoReleaser requires SemVer, so CI presents the derived `vX.Y.Z-rN` only to its
version parser while injecting the full immutable fork tag into the binary and
archive name. The Git and GitHub release authority remains `fork-vX.Y.Z-rN`.

Linux archives are unsigned and integrity-bound by SHA-256. Darwin release mode
is explicit:

- when fork-owned Developer ID variables and Secrets are complete, CI signs and
  notarizes every Darwin binary;
- when no signing identity is configured, CI ad-hoc signs the binaries and
  records `signingMode: adhoc` plus `notarized: false` in the release manifest;
- a partially configured Developer ID mode fails closed and never downgrades.

Expected assets:

```text
agentcookie_fork-vX.Y.Z-rN_darwin_arm64.tar.gz
agentcookie_fork-vX.Y.Z-rN_darwin_amd64.tar.gz
agentcookie_fork-vX.Y.Z-rN_linux_amd64.tar.gz
agentcookie_fork-vX.Y.Z-rN_linux_arm64.tar.gz
checksums.txt
release-manifest.json
```

## Consumer Verification

Install an exact tag rather than an unpinned latest release. Verify the selected
archive against `checksums.txt`, then verify that `release-manifest.json` names
the same tag, source SHA, platform, architecture, and digest. On Darwin,
`codesign --verify --strict` must also pass; the manifest determines whether the
signature is Developer ID/notarized or ad-hoc.

The legacy-named `scripts/install-beta.sh` implements exact-tag download and
checksum verification for Darwin installs. The filename is retained for
compatibility; its verification contract is not beta-only.

## Local Development Artifacts

A local build is suitable for development but is not a public release:

```bash
make build
./bin/agentcookie version
shasum -a 256 ./bin/agentcookie
```

Record local binaries as unsigned/ad-hoc development artifacts. Do not claim
Apple notarization unless the Developer ID, notarytool, and readbacks all pass.

## Deployment Receipt

For every target record:

```text
target:
source_generation_sha:
immutable_tag:
artifact_sha256:
architecture:
signing_status:
installed_path:
version_readback:
process/runtime_readback:
rollback_source:
```

For a running sink, runtime readback includes `/healthz`, the listener address,
and supervisor status. For on-demand session injection it includes the real
Agent Browser E2E result without exposing Cookie values.

## Rollback

Keep the prior binary and config backup until post-deploy verification passes.
Rollback means restoring the prior immutable artifact and restarting its
supervisor. It never means moving the release tag or rewriting the old
generation.
