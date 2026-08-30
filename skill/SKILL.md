---
name: agentcookie
description: >-
  Reuse an authenticated browser identity in either one named agent-browser session or the user's ordinary Google Chrome profile. Use alongside agent-browser for authenticated automation, and when working with agentcookie, Dia/Chrome Cookie SSoT, cross-machine Cookie sync, ordinary-Chrome consumers such as OpenCLI/OpenClaw, source/sink status, or session injection. Inject before the first authenticated navigation so the user does not need to repeat the Cookie workflow.
version: 1.1.0-fork.7
---

# agentcookie

`agentcookie` delivers browser Cookies. Keep the consumer explicit:

- `agent-browser inject` prepares one named, isolated automation session.
- `real_chrome` keeps the user's ordinary Google Chrome profile aligned with a
  source or sink.
- `live_cdp` targets an externally managed automation Chrome, not ordinary
  Chrome.

Do not treat these browser processes or Cookie Stores as interchangeable.

## Default Authenticated Agent Browser Flow

Use this flow automatically when a web task is likely to need an existing login.
Do not wait for the user to repeat the injection instructions.

1. Load the installed agent-browser workflow before browser commands:

   ```bash
   agent-browser skills get core
   ```

2. Reuse an explicitly selected session. Otherwise generate a task-specific one:

   ```bash
   SESSION="$(agent-browser session id --scope worktree --prefix account-audit)"
   ```

   Use `--scope cwd` outside a Git worktree. Concurrent tasks need different
   prefixes. Keep the exact same session name for injection, navigation, and close.

3. Derive the narrow site domain required by the target URL, then inject before
   the first application navigation:

   ```bash
   agentcookie --json agent-browser inject \
     --session "$SESSION" \
     --domain example.com
   ```

   `--domain` is repeatable. Do not omit it and inject the full Cookie set unless
   the user explicitly requests that broader identity surface.

4. Inspect the value-free JSON result. `cookies` must be greater than zero.
   `started: true` means agentcookie started an inactive browser on `about:blank`;
   `started: false` means it reused the active named session.

5. Navigate and automate with the same session:

   ```bash
   agent-browser --session "$SESSION" open https://example.com
   agent-browser --session "$SESSION" snapshot -i
   ```

6. Verify authentication from page state, URL, or title. Do not dump Cookie values.

7. Release the temporary browser when the task is complete:

   ```bash
   agent-browser --session "$SESSION" close
   ```

   Closing is asynchronous. Do not close and immediately reuse the same name;
   choose a fresh task session instead.

## Ordinary Google Chrome Flow

Use this only when the intended consumer is the user's normal Google Chrome
profile, including tools that drive or attach to that profile. OpenCLI/OpenClaw
are control layers; they do not own a separate Cookie Store.

Start with value-free status:

```bash
agentcookie version
agentcookie status --json
```

For an explicit one-off local refresh on macOS:

```bash
agentcookie --json chrome inject \
  --mode offline \
  --from source \
  --domain example.com
```

Use `--from sink` on a receiving machine. Offline mode gracefully stops Chrome
only when it was running, writes Chrome's current host-bound Cookie format, and
restores the prior running state. It does not launch a second browser and needs
no debugging port.

For recurring source or sink delivery, use the corresponding config block:

```yaml
real_chrome:
  enabled: true
  mode: offline
  profile: Default
```

Persistent config, schedules, and target policy are maintenance changes; inspect
and preserve existing topology before editing them. If a previous live setup is
retired, run `agentcookie chrome disable` to remove its no-longer-needed endpoint.

`mode: live` remains available for a visible desktop where Chrome can show and
the user can approve its remote-debugging dialog. Do not select live mode for a
windowless or unattended Mac.

## Source Selection

The default `--from auto` is normally correct:

- With `source.yaml`, agentcookie reads the configured live source browser. This
  fork supports Dia as a Chromium Cookie source.
- With `sink.yaml`, agentcookie reads the latest official plaintext sidecar.
- Use `--from source` or `--from sink` only when both configurations exist and
  the intended authority would otherwise be ambiguous.

Injection is local. Run it on the same machine and as the same OS user that owns
the consumer browser. Tailscale carries source-to-sink synchronization; it does
not make one machine's local browser remotely injectable.

A macOS source may require its GUI LaunchAgent to read the browser Keychain.
When a direct SSH invocation reports a locked login keychain, trigger the
existing GUI supervisor instead of weakening Keychain access or printing secrets.

## Delivery Surface Boundaries

- `agentcookie source` and `agentcookie sink` carry state through the official
  encrypted wire protocol.
- `real_chrome.mode: offline` delivers into ordinary Google Chrome and can
  briefly restart it.
- `live_cdp` delivers into an already-running automation Chrome such as a cloud
  agent runtime.
- `agentcookie agent-browser inject` grants current state to one temporary named
  session.
- Do not start a persistent Chrome, sink, or debug port just for one Agent
  Browser task.

## Preflight And Failure Handling

```bash
agentcookie version
agentcookie status --json
agent-browser --version
```

Apply these rules:

- If `agent-browser inject` or `chrome inject` is missing, the installed
  agentcookie is not the required fork generation. Upgrade from an immutable
  release; do not emulate host-bound writes with ad hoc SQLite edits.
- If `cookies` is zero, verify the domain and source/sidecar freshness. Do not
  silently widen to every domain.
- If the site still redirects to login, the Cookie may be expired,
  device-bound, or the site may keep additional auth state. Report that boundary
  instead of repeatedly exporting broader identity state.
- If a sink has no recent write, repair source/sink delivery first; injection
  cannot manufacture absent Cookies.
- Verify the actual intended consumer. Authentication in Fortress, a temporary
  Agent Browser, or another Chrome profile does not prove ordinary Chrome.
- Never print Cookie values, pairing keys, sidecar contents, or browser database
  rows in logs or chat.

Run pairing, persistent daemon installation, fan-out policy changes, ordinary
Chrome recurring delivery, or source schedule changes only when the user
explicitly asks for setup or maintenance. Preserve existing configuration and
inspect `agentcookie status --json` before changing it.
