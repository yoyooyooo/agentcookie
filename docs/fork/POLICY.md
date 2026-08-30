# Fork Development Policy

## Authority

- Official upstream repository: `https://github.com/mvanhorn/agentcookie.git`
- Upstream default branch: `main`
- Writable fork repository: `git@github.com:yoyooyooo/agentcookie.git`
- Fork upstream mirror branch: `main`
- Active generation: `fork/v1.1.0`
- Baseline tag object: `v1.1.0` at `87103215be1f239b52599039c1f48618dcd5a5f1`
- Baseline commit: `97dd731250b0d9a340f2d0fa776346d807335d60`

The upstream tag is annotated but unsigned. Its official-remote provenance and
peeled commit are verified; a GPG signature is not claimed.

## Invariants

- `main` mirrors official upstream only. Fork-only commits never enter it.
- Fork capabilities target the active `fork/<upstream-version>` generation.
- Each generation begins at an immutable official release tag commit.
- Topic branches own one bounded capability and enter a generation by
  fast-forward or hosted rebase merge only.
- Merge commits are forbidden in the fork-owned range after the baseline.
- Old generation branches and immutable `fork-vX.Y.Z-rN` tags are never moved,
  rebased, deleted, or force-updated.
- Upstream upgrades create a new generation and classify every prior delta as
  `keep`, `rework`, `superseded`, `retire`, or `blocked` before replay.
- The official source/sink wire protocol remains compatible unless a future
  generation explicitly records and approves a protocol fork.
- Source acceptance, release artifact production, and runtime deployment are
  separate transitions with separate evidence.

## Branches

| Ref | Authority | Allowed content |
|---|---|---|
| `upstream/main` | Official project | Upstream development |
| `origin/main` | Mirror | Exact fast-forward mirror of official `main` |
| `fork/vX.Y.Z` | Fork maintainers | Accepted deltas for one release baseline |
| `feature/*`, `fix/*`, `docs/*` | Topic owner | One bounded proposed delta |
| `fork-vX.Y.Z-rN` | Release owner | Immutable accepted generation SHA |

There is no `fork/latest`. The repository default branch may point to the
active generation for discovery, but deployment authority always uses an exact
SHA, immutable tag, and artifact digest.

## Upstream Refresh

Mirror refresh and generation upgrade are separate operations.

1. Fetch official `main` and tags.
2. Verify `origin/main...upstream/main` has no fork-side commits.
3. Fast-forward `origin/main` only when the mirror is unpolluted.
4. Wait for an official release tag before creating a normal generation.
5. Inventory and classify the previous generation's capabilities.
6. Replay one bounded delta at a time against the new release.

An unreleased upstream commit may be projected only as a documented exceptional
hotfix with an explicit retirement condition.

## CI And Review

The generation checks are:

- macOS: `go vet`, race-enabled full tests, build, and real agent-browser E2E;
- Linux: `go vet`, full tests, and build;
- `govulncheck` reachable-vulnerability scan;
- golangci-lint;
- conventional PR title for hosted topic integration.

Workflow configuration runs on `fork/**`. Host branch protection must be read
back before it is claimed; repository files alone do not prove protection is
enabled.

## Releases

Fork releases use `fork-vX.Y.Z-rN`, never the official `vX.Y.Z` namespace.
A release may use either fork-owned Developer ID signing/notarization or an
explicit ad-hoc Darwin mode; the latter must be labeled in the manifest and
must never be described as portable or notarized. Every release records:

- accepted generation SHA;
- immutable fork tag;
- archive SHA-256 digest;
- target architecture;
- signing/notarization status;
- signing/notarization mode for each Darwin artifact;
- deployed binary digest and `agentcookie version` readback;
- rollback tag/artifact.

See [RELEASING.md](RELEASING.md).

## Evidence Homes

- Generation manifests: `docs/fork/generations/`
- Replay ledgers: `docs/fork/generations/*.ledger.yaml`
- Current capability docs: `README.md` and `docs/`
- Build/deployment receipts: generation manifest deployment table and release
  assets/checksums

## Destructive Operations

Do not use `reset --hard`, `clean`, ordinary force push, moving tags, or broad
checkout/restore operations. A conflicted replay may be aborted and restarted
from the generation baseline. Published topic rewrites use a replacement branch
rather than force-updating the old branch.
