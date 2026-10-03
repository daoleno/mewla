# Current Brain Context

## Active Objective

None recorded yet.

## Decisions

- Brain's current host executor is the orchestrator for planning, delegation, review, and final synthesis.
- Delegated Zen Workers use the configured Delegated Executor unless the user explicitly asks for a different executor for that session.
- delegated_executor controls delegated execution and ordinary non-Brain session creation.
- Use a different executor for a session only when the user explicitly mentions or asks for it.
- Switching Brain host executors preserves the visible chat and uses private handoff context.

## Open Threads

- Summarize only context useful for executor handoff. Durable status, ownership, next action, and wait state live in Brain Work.

## Next

- Refresh this projection when handoff context materially changes; do not duplicate Work/Event state manually.
