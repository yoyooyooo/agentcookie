# Agent Browser Session Injection

This fork can inject agentcookie's current cookie set into one isolated
`agent-browser --session` browser instance without keeping an agent browser
running between jobs.

## Roles

| Machine role | Cookie input used by `agent-browser inject` |
|---|---|
| Source | The configured live Chromium source, including Dia |
| Sink | The latest cookie sidecar written by the official sink path |

`--from auto` selects `source` when only `source.yaml` exists and `sink` when
only `sink.yaml` exists. If both files exist, choose explicitly.

The feature does not change source-to-sink transport. Fixed Chrome replication
continues to use the official `/sync` endpoint and port 9999.

## Workflow

Create a stable or job-specific session name, inject before navigation, perform
the browser work, and close the session when finished:

```bash
SESSION="$(agent-browser session id --scope worktree --prefix my-task)"

agentcookie agent-browser inject \
  --session "$SESSION" \
  --domain github.com

agent-browser --session "$SESSION" open https://github.com
# ...automation commands, always with the same --session...
agent-browser --session "$SESSION" close
```

If the session is inactive, `agentcookie` starts it on `about:blank`. Pass
`--start=false` to require an already active session.

A bare `--domain example.com` matches the apex and subdomains. Existing
SQLite-LIKE patterns such as `%github.com` are also accepted. With no domain
flags, every cookie that survives the machine's cookie policy is injected.

## Session targeting

Each agent-browser session is an independent browser instance with its own
cookies and storage. It is not the same concept as a named on-disk Chrome
Profile.

agentcookie asks agent-browser for the selected session's browser WebSocket:

```bash
agent-browser --session "$SESSION" get cdp-url --json
```

It connects directly to that browser-level WebSocket and uses
`Storage.setCookies` against every BrowserContext in that browser. It does not
create, attach to, own, or close a page target. No fixed debug port is required,
so concurrent sessions cannot collide.

Only loopback CDP hosts (`127.0.0.1`, `::1`, or `localhost`) are accepted. A
remote or Tailnet CDP endpoint is rejected even when reachable. A newly launched
browser can publish its WebSocket URL just before accepting another client;
agentcookie retries the browser-level injection for up to five seconds without
creating or closing page targets.

## Source and sink behavior

On the source machine, injection reads and decrypts the configured browser at
call time. For a Dia source:

```yaml
browser:
  name: dia
  profile: Default
```

On a sink machine, injection reads `~/.agentcookie/cookies-plain.db` through
the public sidecar reader. The name is historical: values may be sealed when
agentcookie sidecar sealing is enabled. Cookie domain, path, expiry, Secure,
HttpOnly, persistence, priority, SameSite, scheme, and port metadata are
preserved for CDP injection.

The sink must use an official materializing path that writes the sidecar. A
normal `skip_chrome_sqlite: true` sink still writes the sidecar and remains
compatible. No continuously running Agent Browser or Context Syncer is needed.

## Security boundaries

- The source and sink cookie policies run before session injection.
- Prefer `--domain` so an automation job receives only the identities it needs.
- Cookie values are not written to command output or logs.
- CDP is discovered locally and must resolve to loopback.
- Any process running as the same OS user can potentially control that user's
  agent-browser session; OS-user isolation remains the local trust boundary.
- Device-bound sessions such as Google DBSC may not survive transfer even when
  the Cookie itself is injected successfully.
- Closing an ephemeral session discards its browser state unless agent-browser
  `--restore` is also used. Dia remains the source of truth.

The integration is exercised against `agent-browser 0.33.0`. Run the optional
local E2E after upgrading agent-browser:

```bash
AGENTCOOKIE_AGENT_BROWSER_E2E=1 \
  go test ./internal/cli -run '^TestAgentBrowserSessionInjectE2E$' -count=1
```
