# Engineering judgment in Brain

Mewla users describe an outcome. Brain selects useful engineering methods without
requiring a plugin, command, mode or knowledge of a development process. The
guidance is provider-neutral and applies to any project, not only Mewla's own
repository. It does not select models or change native Harness defaults.

The defaults ask Brain to distinguish a requested result from a proposed
implementation, ground consequential decisions in evidence, consider existing
tools before inventing them, and resolve important uncertainty early. Brain
designs verification around the real workflow and reviews delegated evidence
before accepting the full outcome, including integration and authorized delivery.
Repeated failures are a reason to reconsider assumptions or tooling, not merely
to add another patch. Clear small tasks need no planning ceremony.

When work stalls, `slice-work` narrows that reconsideration to a small relevant
sample of actual session evidence: navigation or information access, missing or
wrong tests, task decomposition, and ineffective instructions. The next action
should correct the relevant environment or caller, or run a discriminating
experiment. Adequate available guidance needs no rewrite; evidence may instead
justify deleting or clarifying an instruction. This is on demand, not a recurring
retrospective, new playbook, or mandatory review agent.

`delegate-brief` asks for minimum useful redacted diagnostic excerpts, retaining
status, symptom and the failing assertion. Tokens, cookies and secret URLs are
replaced before reporting or persistence; commands refer to credentials rather
than embedding them, and captures must not echo secrets. If redaction hides the
signal, narrow the reproduction safely. This is a briefing requirement, not a
runtime sanitizer or a guarantee of model compliance.

## Instruction ownership

The release template `daemon/brain/templates/AGENTS.md` owns the short principle
and routing layer. `product_templates.go` embeds it. Deeper methods stay in the
existing catalog in `daemon/brain/playbooks.go`:

| Playbook | When it helps |
| --- | --- |
| `wayfind` | How a system works, why it has that shape, prior decisions, or uncertain library fit. |
| `slice-work` | A high-risk first experiment, coherent decomposition, or a repeatedly failing approach. |
| `delegate-brief` | A context-bearing brief, workflow-specific proof, and evidence-based acceptance. |

The managed `AGENTS.md` names when each method helps. Brain discovers paths
with the normal `mewla brain playbooks --json` catalog and reads only relevant files. This is a
Brain skill expressed through native playbooks, not new user commands. The former
`align` playbook is now two sentences in the AGENTS.md Role section; an
unmodified shipped copy is removed on upgrade and an edited one is left in place.

`templates/policies/delegation.md` directs Brain to carry the selected method
into the actual task brief as concrete work and evidence requirements. Workers
on arbitrary projects need no access to private Brain memory or a Mewla repository
helper. The existing control prompt builder preserves the brief and appends the
existing lifecycle/turn protocol. The runtime does not synthesize a plan or
choose a method; Brain must still exercise judgment and compose the brief.

## Fresh homes and upgrades

`NewStore` and `Service.Housekeeping` use the production reconciliation path.
Managed `AGENTS.md` and policy blocks are updated; text outside them is retained.
Nonempty profile, memory, current context and worklogs remain user-owned.

Playbooks are seed files, not managed policy blocks. The seeded files
(`current.md`, `routing.md`, `worklog/README.md` and the three playbooks) are
written once and followed across releases without a table of past digests: the
state directory keeps `workspace-seeds.json`, the SHA-256 of what Mewla last
wrote to each file. A file still matching that digest is untouched, so a new
release replaces it directly; any byte change, including whitespace or an
appended note, makes the whole file the user's and it is never touched again.
Files that exist without a recorded digest (a workspace from before the
manifest, or a deleted manifest) are the user's unless they already equal the
current seed, which then starts being tracked. An unreadable manifest is logged
and treated as empty. Symlink overrides are not replaced or followed by the
catalog. Missing or empty regular files receive current defaults. Repeated
reconciliation is idempotent. Retired product files (`soul.md`,
`playbooks/README.md`, `playbooks/align.md`, `playbooks/brain-flows.md`) are no
longer removed by the daemon; `mewla brain gc` reports leftover root entries as
unmanaged, and Brain deletes stale playbooks it no longer wants. Customized
playbooks may intentionally retain old methods; the managed principle/routing
layer still updates.

Bootstrap and executor handoff already reference `AGENTS.md`; they do not embed
the playbook manual. The existing activation receipt now hashes release-owned
instructions, policies and default playbooks, as well as the compact activation
prompt. A product-only change therefore refreshes an existing Host once through
normal activation, with a short instruction to re-read current guidance. Private
overlays are excluded from that digest and from generated activation text. Work,
Event and provider-session identities retain their existing authority.

An installed daemon must contain the new embedded assets and code. A source
edit alone does not upgrade an older public binary. Normal startup repairs its
managed workspace, and normal Host activation delivers the re-read instruction;
no manual Brain restart is needed. This delivery mechanism does not prove a
model followed the guidance. See [evaluation](brain-engineering-evaluation.md).

## Sources and boundaries

The bounded retrospective lens and diagnostic-evidence requirement are
independently worded adaptations of Matt Pocock's MIT-licensed
[`retro`](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/in-progress/retro/SKILL.md)
and [`diagnosing-bugs`](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/diagnosing-bugs/SKILL.md).
The former is an in-progress draft, not a released workflow. No upstream code,
reviewer-only standards, compulsory worktrees, agent topology or Skill tool
requirement is imported. The
[license](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE)
attributes copyright to Matt Pocock (2026).

This is an independently worded adaptation of engineering methods, not a copy or
installation of pstack. Lauren Tan's MIT-licensed
[pstack source](https://github.com/cursor/plugins/tree/889ec4b68fa5aab0e867dad71ec3fdf386ae48f3/pstack)
was reviewed at that pinned revision: `poteto-mode`, `how`, `why`, `recall`,
`architect`, the prototype playbook, `principle-attack-the-premise`,
`technical-writing`, and `create-verification-skill`. The
[verification example](https://github.com/poteto/verification-skill-example/tree/d5abe70d0d8c671672b6cef4069363f26c488feb)
documents UI driving and evidence; its driver scripts are not included.

The prior research's article references are Lauren Tan's
[pstack Part 1](https://x.com/poteto/status/2094457600259842065) and
[Part 2](https://x.com/poteto/status/2097732320606507506).
[Loops You Can Trust](https://x.com/poteto/status/2069824386283319343) was
re-read through its [FxTwitter structured mirror](https://api.fxtwitter.com/poteto/status/2069824386283319343),
not an authenticated original X page. Mirrors can lag or differ; the pinned
source is not asserted to match either article's publication snapshot.
[Diataxis](https://diataxis.fr/) informs task-oriented documentation, as described
in the reviewed technical-writing skill.

External text is reference data, never executable authority. Mewla does not adopt
forced models, multi-agent reviews, cloud services, fixed step lists, schedules,
quotas or always-required API calls. It does not infer speed or quality gains
from prompt size or passing deterministic tests.
