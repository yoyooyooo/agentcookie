# Agent prompt: configure agentcookie

Use this prompt only for first-time setup or repair. Normal Agent Browser tasks
should use the session-injection flow in `../SKILL.md` without changing daemons.

> Configure agentcookie so my authoritative Mac browser can send Cookies over
> Tailscale to the machines and browser surfaces where I run automation. First
> inspect the existing agentcookie version, source/sink configuration,
> supervisors, sidecar, ordinary Chrome configuration, automation Chrome
> endpoints, and Tailscale reachability. Confirm which machines need ordinary
> Google Chrome versus an externally managed automation Chrome before changing
> persistent services. Preserve the official source/sink wire protocol. After
> setup, verify each intended consumer without printing Cookie values.

The agent should:

1. Run `agentcookie version` and `agentcookie status --json` on every involved
   machine before changing configuration.
2. Confirm the authoritative source browser and every intended sink. Dia is a
   supported source in this fork; do not assume Chrome is the SSoT.
3. Inventory each browser consumer separately:
   - ordinary Google Chrome profile: `real_chrome`;
   - externally managed automation Chrome: `live_cdp`;
   - temporary named session: `agent-browser inject`.
4. Reuse existing Tailscale hostnames, pairing state, supervisors, and healthy
   endpoints. Do not start a competing browser or debug port.
5. For an unattended macOS ordinary Chrome consumer, use:

   ```yaml
   real_chrome:
     enabled: true
     mode: offline
     profile: Default
   ```

   Keep `skip_chrome_sqlite: true` on a sink: `real_chrome` owns its bounded,
   host-bound write. If live mode is retired, run `agentcookie chrome disable`.
6. Keep Cookie-only and per-target domain policy boundaries when configured.
   Widen a target to sync-all only with explicit operator intent.
7. Verify the source/sink transport without printing Cookie values or pairing
   keys. A current sidecar is delivery evidence, not final browser acceptance.
8. Verify every intended fixed browser through its real consumer. Authentication
   in Fortress or an Agent Browser session does not prove ordinary Chrome.
9. Prove the on-demand path with a new session and a narrow domain:

   ```bash
   SESSION="$(agent-browser session id --scope cwd --prefix agentcookie-proof)"
   agentcookie --json agent-browser inject --session "$SESSION" --domain example.com
   agent-browser --session "$SESSION" open https://example.com
   agent-browser --session "$SESSION" close
   ```

10. Report immutable release versions, supervisors, schedules, health, last
    write counts, ordinary-Chrome mode/restart results, and any unverified
    boundary. Do not describe a Git tag or local build as a signed GitHub
    Release.
