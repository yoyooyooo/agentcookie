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

Use an immutable fork-specific tag:

```bash
fork-v1.1.0-r1
```

Before creating it, show and record the exact candidate:

```bash
git rev-parse fork/v1.1.0
git log -1 --oneline fork/v1.1.0
git rev-list --merges v1.1.0..fork/v1.1.0
git diff --check v1.1.0..fork/v1.1.0
```

Never use or move the official `v1.1.0` namespace. A correction receives
`fork-v1.1.0-r2`; `r1` remains immutable.

## CI Release Path

The GitHub workflow listens only for `fork-v*`. It remains gated by the
repository variable `RELEASE_CI_ENABLED=true`. Portable Darwin releases also
require repository variables `AGENTCOOKIE_SIGN_IDENTITY`, `APPLE_ID`, and
`APPLE_TEAM_ID`, plus the certificate and notary Secrets used by the workflow.
The workflow fails closed when this fork-owned signing authority is absent; it
never falls back to the upstream maintainer's identity. Repository config
cannot prove those host settings, so inspect them before tagging.

The expected release contains Linux and Darwin archives plus `checksums.txt`.
The release title includes `fork` to avoid confusing it with an official
agentcookie release.

## Local Development Artifacts

A local build is suitable for the same operator's machines but is not a public
signed release:

```bash
make build
./bin/agentcookie version
shasum -a 256 ./bin/agentcookie
```

On Linux:

```bash
go build -ldflags \
  "-X github.com/mvanhorn/agentcookie/internal/cli.Version=fork-v1.1.0-r1" \
  -o ~/.local/bin/agentcookie ./cmd/agentcookie
sha256sum ~/.local/bin/agentcookie
```

Record these as unsigned/ad-hoc development artifacts. Do not claim Apple
notarization unless `codesign`, `notarytool`, and Gatekeeper readback all pass.

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
and process supervisor status. For on-demand session injection it includes the
real Agent Browser E2E result without exposing Cookie values.

## Rollback

Keep the prior binary and config backup until post-deploy verification passes.
Rollback means restoring the prior immutable artifact and restarting its
supervisor. It never means moving the release tag or rewriting the old
generation.
