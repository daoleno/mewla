# Brain Delegation Policy

Mewla Workers are visible execution Sessions started with mewla worker spawn; they are not provider-native or Codex-internal subagents.

## Brief

- Give one Worker one coherent concern: the outcome, cwd, context it cannot cheaply discover (findings with sources, prior decisions, the open unknown), acceptance criteria, constraints, the proof you need and the report shape. Link paths instead of pasting transcripts or this policy; skip empty sections and rules the Worker protocol already carries. playbooks/delegate-brief.md covers proof design.
- Reuse a capable Worker across stages. Run Workers in parallel only for concerns that share no fragile state or open decision.

## Review

- Check each result against its acceptance criteria: read a real sample of the implementation and the risky interfaces, and compare the reported evidence with the full user outcome. A passing check proves only what it exercised; missing interaction, integration or requested delivery is still a gap.
- Required repository gates are part of acceptance; a passing subset does not replace a full-task requirement.
- Send a focused follow-up for a concrete gap. If the same premise keeps failing, change the approach or the test tool instead of commissioning another patch. Ask for independent review only for a specific risk.

## Continuation

Continue a Worker with mewla worker send -id <session> -text <follow-up> --work-id <work>; that is the whole step, with no separate resolve. Worker reports and provider errors are evidence for deciding to continue, accept, cancel or wait, not acceptance.

When delivery is uncertain, weigh the cost of a duplicate effect against the evidence before checking or sending again.

## Machine resources

A resource_pressure event reports sustained machine pressure: crossed signals, headroom, the largest Workers and orphan processes, and queued Work. Run mewla resources --json only when you will act on it. Brain decides whether to defer dispatch, ask a Worker to release a tool, or close an owned Worker; the daemon never kills or throttles. Release one tool with mewla worker release -id SESSION -pid PID -start START from that snapshot. A newer event or recovery closes unreviewed pressure Work, and recovery itself is not delivered.
