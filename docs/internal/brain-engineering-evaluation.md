# Evaluate Brain engineering guidance

Use the five cases in
[`engineering-scenarios.json`](../../daemon/brain/testdata/engineering-scenarios.json).
They contain user requests, bounded context, acceptable judgments, rejection
signals and illustrative briefs. The examples are not golden model outputs;
judge decisions and proportionality, not exact wording or playbook names.

| Case | Decision to evaluate |
| --- | --- |
| Trivial fix | Act with focused verification; no forced planning or reconfirmation. |
| Uncertain cross-platform library | Check local reuse and relevant API, version, license and Android/iOS evidence; test the riskiest unknown first. |
| Interactive UI bug | Reproduce and verify the actual interaction and shared contract; distinguish both-platform behavior from device evidence. |
| Apparently green helper-only result | Withhold acceptance of the full outcome and request the missing proof, without redundant polling. |
| Repeated approach failure | Revisit the shared premise and unsuitable observation tool; run a discriminating experiment rather than another retry patch. |

## Deterministic delivery tests

From `daemon/`, run the existing Go test tools with bounded parallelism:

```sh
GOMAXPROCS=2 go test -p 1 ./brain ./cmd/mewla -run 'Test(Engineering|HostContractDigest|HostActivation|BrainWorkerRole|WorkerPrompt|GeneratedWorkerPrompt|Handoff|Prompt)' -count=1
GOMAXPROCS=2 go test -p 1 ./...
```

The focused tests create temporary Brain homes through `NewStore`, repair
through `Service.Housekeeping`, read through `PlaybookCatalog` and
`ReadWorkspaceFile`, and activate hosts through `EnsureHostSnapshot`. They check
idempotence, preserved overlays, lazy prompt surfaces and refresh after an old
role-only activation. Control tests send the illustrative briefs through
`HandleControlRequest` into visible owned Work with native Pi/Codex commands
and captured submission payloads. The provider/terminal boundary is a test fake;
there are no model calls. String assertions prove delivery and ownership, not
reasoning quality, UI behavior or task completion by a real Worker.

`TestEngineeringMethodDeltaSeedDelivery` additionally exercises both startup and
housekeeping for the exact pre-delta slice-work/delegate-brief seeds, missing and
empty files, whitespace edits, custom notes and unknown versions, then checks
idempotence. Existing tests retain older-seed, symlink, private-overlay and Host
refresh coverage. Catalog reads assert the bounded retrospective and safe
diagnostic-evidence requirements are in the two lazy methods, not standing
policy or provider bootstrap/handoff prompts. These checks neither redact a
model's output nor demonstrate that Brain chooses a better next action.

## Model walkthrough

Give a reviewer the generated standing guidance and each case's user/context
fields. Ask what evidence changes the next decision, which method (if any) to
load, what to delegate, and what would count as acceptance. Compare the answer
with the expected judgments and rejection signals. Record concrete omissions
and unnecessary work. This is a qualitative walkthrough, not a live Brain run
or a measurement of speed or productivity.
