# Usage and resources

Two pages answer "what are my agents consuming?": **Stats** for tokens and
cost, **Resources** for CPU, memory and disk on the computer. Tap the cat for
the computer page; both are there. They show the current computer only; Mewla
never adds several computers together.

## Stats

Stats reads the agents' own history on the computer. Its title is the total
cost for the range you pick, with spend by day as a chart,
then tokens, sessions and cost per model and per project. Tap a model for
input, output and cache tokens, the price source and when it was refreshed, or
a day in the chart for that day.

Costs come in four kinds, kept apart:

| Kind | Meaning |
| --- | --- |
| Reported | The agent's history recorded an actual charge |
| Estimated | Priced from a public reference price list; marked `≈` |
| Mixed | Part reported, part estimated |
| Unknown | No known price; shown as `—`, never as zero |

Estimates use public reference prices; your provider or gateway may bill
differently. The prices come from the public models.dev catalog, which Mewla
downloads once a day and shares with the model picker. Until the first download
succeeds Mewla uses a short price table built into the daemon; the price source
then reads `built-in` instead of `models.dev`, and the daemon logs that it is
doing so.

When Codex is signed in with ChatGPT and uses **Official login**, Stats also
shows the plan's usage windows (for example 5 hours and 1 week) with the share
left. Mewla asks Codex for them through Codex's own app-server (`codex app-server`,
`account/rateLimits/read`); it never reads Codex's sign-in file or sends its token
anywhere.

Per agent:

- **Claude Code**: read from `~/.claude/projects` (or `$CLAUDE_CONFIG_DIR/projects`)
  every five minutes, including traffic through a Custom Gateway. Costs are
  reference estimates, not your subscription bill.
- **Codex**: each request is priced at its own context size, so long-context
  tiers apply per request.
- **OpenCode**: read from OpenCode 2's background service through its own
  API (`opencode api`).
- **Pi**: read from `~/.pi/agent/sessions` (or `$PI_CODING_AGENT_DIR/sessions`).

## Resources

Resources is a live view of the computer, refreshed every five seconds while it
is open:

- The headline says how much pressure the computer is under, and which
  signals crossed a threshold.
- **CPU, Memory, Disk and Pressure** tiles. On Linux, Pressure is PSI. On a Mac
  there is no PSI: the tile shows the system's own memory pressure level
  (Normal, Warning or Critical) instead.
- **Using this machine**: processes grouped by owner (Workers, orphaned
  Workers, Brain, Docker and you).

When pressure turns elevated or critical, Brain is told and decides what to do,
such as waiting before it starts more Workers or closing one. Mewla itself never
kills or throttles a process.

A state change needs 20 seconds of sustained pressure. To change a threshold,
put the values you want in `~/.mewla/resource-telemetry.json`; it is reread on
every sample, no restart needed:

```json
{
  "available_memory_percent": { "elevated": 15, "critical": 7 },
  "cpu_busy_percent": { "elevated": 90, "critical": 98 },
  "disk_free_percent": { "elevated": 10, "critical": 3 }
}
```
