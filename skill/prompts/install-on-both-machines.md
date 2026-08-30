# Agent prompt: configure agentcookie

Use this prompt only for first-time setup or repair. Normal Agent Browser tasks
should use the session-injection flow in `../SKILL.md` without changing daemons.

> Configure agentcookie so my authoritative Mac browser can send Cookies over
> Tailscale to the machines where I run browser automation. First inspect the
> existing agentcookie version, source/sink configuration, supervisors, sidecar,
> and Tailscale reachability. Confirm source and targets with me before changing
> persistent services. Preserve the official source/sink wire protocol. After
> setup, verify value-free status and inject a fresh named agent-browser session
> for one narrow test domain; navigate with that same session and close it.

The agent should:

1. Run `agentcookie version` and `agentcookie status --json` on every involved
   machine before changing configuration.
2. Confirm the authoritative source browser and every intended sink. Dia is a
   supported source in this fork; do not assume Chrome is the SSoT.
3. Reuse existing Tailscale hostnames, pairing state, supervisors, and fixed
   Chrome endpoints where healthy. Do not start a competing browser or debug port.
4. Keep Cookie-only and per-target domain policy boundaries when configured.
5. Treat the persistent fixed-browser sync path separately from temporary
   Agent Browser sessions.
6. Verify the source/sink path without printing Cookie values or pairing keys.
7. Prove the on-demand path with a new session and a narrow domain:

   ```bash
   SESSION="$(agent-browser session id --scope cwd --prefix agentcookie-proof)"
   agentcookie --json agent-browser inject --session "$SESSION" --domain example.com
   agent-browser --session "$SESSION" open https://example.com
   agent-browser --session "$SESSION" close
   ```

8. Report installed versions, supervisors, schedules, health, last write counts,
   and any unverified boundary. Do not describe a Git tag or local build as a
   signed GitHub Release.
