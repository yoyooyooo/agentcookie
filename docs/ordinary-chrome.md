# Ordinary Google Chrome delivery

`real_chrome` delivers a source or sink Cookie set into the user's normal
Google Chrome Default profile. It is a separate surface from an externally
managed automation browser (`live_cdp`) and from one isolated Agent Browser
session (`agent-browser inject`).

## Properties

- Uses the existing `/Applications/Google Chrome.app` process and Default
  profile.
- Writes through Chrome's live DevTools endpoint. It never edits Chrome's
  Cookie SQLite database.
- Does not launch a second browser or a second profile.
- Chrome applies Cookie validation and performs its own persistence.
- The endpoint binds to loopback. Any same-user local process can control an
  enabled debugging browser, so only enable this on a single-user trusted
  machine.

## Prepare Chrome once

On macOS:

```bash
agentcookie chrome status --json
agentcookie chrome enable
agentcookie chrome status --json
```

`chrome enable` gracefully quits and relaunches Google Chrome once. It sets
Chrome's persisted `devtools.remote_debugging.user-enabled` preference and
waits for `DevToolsActivePort`. Existing windows remain Chrome-owned and are
restored by Chrome.

Chrome can show a local "Allow remote debugging" dialog when agentcookie
connects. `auto_approve: true` uses macOS Accessibility to approve that Chrome
UI. The caller must already have Accessibility permission; otherwise leave it
false and approve the dialog manually.

## Source machine

A source can also consume its own authoritative browser identity:

```yaml
# ~/.config/agentcookie/source.yaml
browser:
  name: dia
  profile: Default

real_chrome:
  enabled: true
  auto_approve: true
```

Each `agentcookie source --once` or `--watch` cycle injects the source-policy
Cookie set into ordinary Chrome and independently attempts every configured
remote target. A local injection failure does not prevent remote targets from
being attempted, but the source command exits non-zero so the failure is
observable.

## Sink machine

A macOS sink can receive through the normal encrypted source/sink protocol and
then deliver locally:

```yaml
# ~/.config/agentcookie/sink.yaml
skip_chrome_sqlite: true
real_chrome:
  enabled: true
  auto_approve: true
```

The sink first materializes the official sidecar, then injects ordinary Chrome.
The `/sync` response and `agentcookie status --json` report the value-free
result. Sidecar success remains durable even if Chrome is temporarily closed.
The next source push retries delivery.

## One-off injection

```bash
agentcookie chrome inject --from source --domain facebook.com
agentcookie chrome inject --from sink --domain facebook.com
```

`--domain` is repeatable. Configured `domain_filter` entries use SQLite-LIKE
host patterns such as `example.com` and `%.example.com`. Empty means every
Cookie that already passed the source or sink policy.

## Verification

Verify from the same ordinary Chrome profile, without printing Cookie values:

```bash
agentcookie chrome status --json
agentcookie status --json
```

Then use the actual consumer attached to that Chrome, or navigate in a normal
Chrome tab. Reaching an authenticated page is the acceptance signal; the
presence of rows in a sidecar is not sufficient.
