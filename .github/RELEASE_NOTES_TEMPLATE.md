# agentcookie fork-v1.1.0-r4

Maintained fork release based on official agentcookie `v1.1.0` at
`97dd731250b0d9a340f2d0fa776346d807335d60`.

## Fork Capabilities

- Dia can be the authoritative macOS Cookie source.
- One source read can fan out to independently paired named sinks.
- `AGENTCOOKIE_COOKIES_ONLY=1` excludes Local Storage, IndexedDB, and the
  secrets bus.
- `agentcookie agent-browser inject --session <name>` grants a domain-scoped
  Cookie set to one temporary Agent Browser session through agent-browser's
  native JSON batch stdin protocol.
- The official v1 source/sink envelope and `/sync` endpoint remain compatible.

## Release Integrity

This is the first fork generation distributed as CI-built release archives.
Download the archive for the target architecture together with
`checksums.txt` and `release-manifest.json`, then verify SHA-256 before
installation.

```bash
shasum -a 256 -c checksums.txt --ignore-missing   # macOS
sha256sum -c checksums.txt --ignore-missing       # Linux
```

`release-manifest.json` binds the immutable tag, peeled source SHA, archive
digests, builders, and Darwin signing/notarization status. A Darwin archive
whose manifest says `signingMode: adhoc` is checksummed but is not Developer ID
signed or Apple-notarized.

## Installation Boundary

The module intentionally retains the official Go module path to keep upstream
replay small. Install this fork from its release archive or an exact generation
clone; do not use the fork URL with `go install`.

## Runtime Model

- A source workstation reads the configured Chromium-family browser, including
  Dia, and may fan out to independently paired sinks.
- A sink materializes the official sidecar and may maintain a fixed Chrome
  identity through configured delivery adapters.
- Any source or sink can grant the current Cookie state to one named Agent
  Browser session on demand, then close that browser after the automation job.

## Honest Limits

- Google/Workspace DBSC identity generally requires local browser sign-in.
- A same-user process can control an active Agent Browser session; this remains
  the local OS trust boundary.
- Sink sidecar values are plaintext unless sidecar sealing is enabled.
- Source acceptance, release assets, and deployment remain separate claims.

## Evidence

- [Fork policy](https://github.com/yoyooyooo/agentcookie/blob/fork/v1.1.0/docs/fork/POLICY.md)
- [Generation manifest](https://github.com/yoyooyooo/agentcookie/blob/fork/v1.1.0/docs/fork/generations/v1.1.0.md)
- [Agent Browser sessions](https://github.com/yoyooyooo/agentcookie/blob/fork/v1.1.0/docs/agent-browser-sessions.md)
