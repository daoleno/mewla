# Brain Delegation Policy

Follow the Brain/Worker role in AGENTS.md. Zen Workers are visible execution sessions; provider-native agents and Codex internal subagents are different concepts.

## Brief And Review

- Give one Worker a coherent concern, cwd, necessary context, observable acceptance criteria, safety constraints, verification and expected report. Include current/desired behavior and interfaces when they add information; omit empty sections and repeated standing rules.
- Carry the selected method into the actual brief: relevant evidence and prior decisions, the consequential unknown, reuse candidates or a small discriminating experiment, and workflow-specific proof where needed. Pass useful paths or a concise finding with provenance, not whole transcripts or this policy. Consult playbooks/delegate-brief.md when acceptance or evidence design is unclear.
- Reuse the same viable Worker across stages. Parallelize independent concerns only when they do not share fragile state or unresolved decisions.
- Inspect every delegated result against acceptance criteria before integration, including a meaningful implementation sample and risky interfaces, and reconcile the reported evidence with the full user outcome. Send a focused follow-up for a concrete gap; otherwise record the result and close the owned session when the larger task is done. A successful helper proves only what it exercised; missing UI interaction, integration or authorized delivery remains a gap. Reconsider a repeatedly failing premise or unsuitable test tool before commissioning another patch. Independent review is useful for a concrete risk, not a default multi-agent ritual.
- Scale verification to risk. Use meaningful behavior checks, complete required repository gates, and broaden or repeat only for new edits, failures or unresolved concerns. Do not replace a full-task requirement with a passing subset.

## Continuation

Reuse a Worker with zen worker send -id <session> -text <follow-up> --work-id <work>. Runtime mints and persists the turn identity and binds accepted execution; there is no second continuation command. Review the returned facts and decide whether to continue, accept, cancel or wait. Worker reports and provider errors are evidence, not acceptance of the larger objective.

For uncertain delivery, weigh duplicate effects and available evidence before choosing reconciliation or a new attempt. Runtime preserves both outcomes without imposing that choice.

## Machine resources

`zen_work_event` with `kind=resource_pressure` carries a structured `resource_pressure` payload: sustained state changes, crossed signals and trend, machine totals, top attributed Workers/tools, orphaned processes, and queued/in-flight Work. Read `zen resources --json` for the full current snapshot and history. The daemon observes and notifies; Brain decides whether to defer dispatch, ask a Worker to release tools, or close an owned Worker. Recovery is informational. No pressure event authorizes automatic killing or throttling by the daemon. To release one tool without closing its Worker, use `zen worker release -id SESSION -pid PID -start START` from the current snapshot; it preserves the provider and refuses stale/unowned identities. Completed Work cleanup still terminates its exact owned descendants.
