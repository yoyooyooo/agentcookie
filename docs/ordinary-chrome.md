# Ordinary Google Chrome delivery

`real_chrome` delivers a source or sink Cookie set into the user's normal
Google Chrome profile. It is distinct from an externally managed automation
browser (`live_cdp`) and from one isolated Agent Browser session
(`agent-browser inject`).

## Modes

### Offline mode

Offline mode is the unattended path for an always-on workstation or macOS
sink:

1. Detect whether Google Chrome is running.
2. If running, request a normal quit and fall back to `SIGTERM` only when a
   modal UI prevents AppleScript from completing.
3. Write Chrome 127+'s host-bound plaintext shape
   (`SHA256(host_key) || value`) under the destination Chrome Safe Storage key.
4. Preserve Chrome's own Cookie schema version.
5. Relaunch Chrome only when it was running before the sync.

Chrome is never running while its Cookie database is open. The write is one
SQLite transaction, and Chrome performs its normal validation on the next
load. This mode starts no second browser and needs no debugging endpoint.

```yaml
real_chrome:
  enabled: true
  mode: offline
  profile: Default
```

A sync can briefly restart an active Chrome. Schedule recurring source pushes
at an appropriate time for interactive workstations.

### Live mode

Live mode writes through Chrome's DevTools endpoint and never opens the Cookie
database. It requires a visible Chrome permission dialog when a client
connects, so it is appropriate only where a user can approve that UI.

Prepare Chrome once:

```bash
agentcookie chrome enable
agentcookie chrome status --json
```

Then configure:

```yaml
real_chrome:
  enabled: true
  mode: live
  auto_approve: true
```

`auto_approve` uses macOS Accessibility and is best effort. Chrome's permission
model remains authoritative; an environment with no visible browser window
should use offline mode. After switching from live to offline mode, remove the
no-longer-needed endpoint with `agentcookie chrome disable`.

## Source machine

A source can consume its own authoritative browser identity. For example, Dia
can remain the source of truth while ordinary Chrome receives the same set:

```yaml
# ~/.config/agentcookie/source.yaml
browser:
  name: dia
  profile: Default

real_chrome:
  enabled: true
  mode: offline
  profile: Default
```

Each `agentcookie source --once` or `--watch` cycle attempts local ordinary
Chrome delivery and independently attempts every configured remote target. A
local failure does not prevent remote attempts, but the source command exits
non-zero so the failure remains observable.

## Sink machine

A macOS sink can receive through the encrypted source/sink protocol and then
deliver locally:

```yaml
# ~/.config/agentcookie/sink.yaml
skip_chrome_sqlite: true
real_chrome:
  enabled: true
  mode: offline
  profile: Default
```

The sink materializes the official sidecar first, then updates ordinary
Chrome. Keep `skip_chrome_sqlite: true`: `real_chrome.mode: offline` owns the
bounded host-bound write and must not be combined with the legacy sink SQLite
format. Sidecar success remains durable if Chrome delivery fails; the next
source push retries.

## One-off injection

```bash
agentcookie chrome inject --mode offline --from source --domain facebook.com
agentcookie chrome inject --mode offline --from sink --domain facebook.com
```

`--domain` is repeatable. Configured `domain_filter` entries use SQLite-LIKE
host patterns such as `example.com` and `%.example.com`. Empty means every
Cookie that already passed the source or sink policy.

## Security boundaries

- Offline mode handles decrypted Cookie values in memory and writes only the
  target Chrome's encrypted database under that Chrome's Keychain-derived key.
- Live mode exposes full browser control on a loopback endpoint while enabled;
  any same-user local process can attempt to connect, subject to Chrome's
  approval UI.
- Neither mode prints Cookie values. Status and sync responses report counts,
  mode, restart state, and errors only.
- Device-bound sessions such as Google DBSC cannot be transferred by either
  mode.

## Verification

```bash
agentcookie status --json
```

Then use the actual consumer attached to ordinary Chrome or navigate in a
normal Chrome tab. Reaching an authenticated page is the acceptance signal;
rows in a sidecar or Cookie database alone are not sufficient.
