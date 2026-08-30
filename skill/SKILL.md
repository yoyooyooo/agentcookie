---
name: agentcookie
description: >-
  Reuse an existing authenticated browser identity in one named agent-browser session. Use alongside agent-browser whenever a website task may need the user's current login, or when working with agentcookie, Dia/Chrome Cookie SSoT, cross-machine Cookie sync, source/sink status, or session injection. Inject before the first authenticated navigation so the user does not need to repeat the Cookie workflow.
version: 1.1.0-fork.4
---

# agentcookie

`agentcookie` delivers browser Cookies. `agent-browser` owns navigation and page
automation. Use both for authenticated browser work: agentcookie prepares one
isolated session, then every browser command reuses that session.

## Default Authenticated Browser Flow

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

   `--domain` is repeatable for an application that genuinely spans multiple
   Cookie domains. Do not omit it and inject the full Cookie set unless the user
   explicitly requests that broader identity surface.

4. Inspect the value-free JSON result. `cookies` must be greater than zero.
   `started: true` means agentcookie started an inactive browser on `about:blank`;
   `started: false` means it reused the active named session.

5. Navigate and automate with the same session:

   ```bash
   agent-browser --session "$SESSION" open https://example.com
   agent-browser --session "$SESSION" snapshot -i
   ```

6. Verify authentication from page state, URL, or title. Do not dump Cookie values
   as a verification step.

7. Release the temporary browser when the task is complete:

   ```bash
   agent-browser --session "$SESSION" close
   ```

   Closing is asynchronous. Do not close and immediately reuse the same name;
   choose a fresh task session instead. Inject directly into an already-active
   session when refresh is needed.

## Source Selection

The default `--from auto` is normally correct:

- With `source.yaml`, agentcookie reads the configured live source browser. This
  fork supports Dia as a Chromium Cookie source.
- With `sink.yaml`, agentcookie reads the latest official plaintext sidecar.
- Use `--from source` or `--from sink` only when both configurations exist and
  the intended authority would otherwise be ambiguous.

Injection is local. Run it on the same machine and as the same OS user that runs
agent-browser. Tailscale carries source-to-sink synchronization; it does not make
one machine's local Agent Browser session remotely injectable. For a remote task,
run both commands on that target machine.

## Relationship To Fixed Chrome Sync

The long-lived source/sink path and on-demand Agent Browser path are separate:

- `agentcookie source` and `agentcookie sink` keep fixed browser or sidecar state
  synchronized through the official wire protocol.
- `agentcookie agent-browser inject` grants that current state to one named,
  temporary session.
- Do not start a persistent Chrome, sink, or debug port just for one Agent Browser
  task. An inactive session starts on demand and should be closed afterward.

## Preflight And Failure Handling

Start diagnosis with value-free readbacks:

```bash
agentcookie version
agentcookie status --json
agent-browser --version
```

Then apply these rules:

- If `agent-browser inject` is missing, the installed agentcookie is not this fork
  generation. Do not emulate it by editing Chrome SQLite or opening a second CDP
  connection.
- If `cookies` is zero, verify the domain and sidecar/source freshness. Do not
  silently widen to every domain.
- If the site still redirects to login, the Cookie may be expired or device-bound.
  Report that boundary instead of repeatedly exporting broader identity state.
- If the sink has no recent write, repair source/sink delivery first; session
  injection cannot manufacture absent Cookies.
- Never print Cookie values, pairing keys, sidecar contents, or browser database
  rows in logs or chat.

Run pairing, persistent daemon installation, fan-out policy changes, or source
schedule changes only when the user explicitly asks for setup or maintenance.
Preserve existing configuration and inspect `agentcookie status --json` before
changing it.
