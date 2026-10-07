# Brain and the Work Lifecycle

Brain is the sole master orchestrator above delegated Sessions. Given a user
goal and boundaries, it decomposes and orders the work, chooses or reuses
scoped Sessions, reviews their outputs, and advances the next runnable concern
without requiring the user to type `continue`. Delegated Agents execute scoped
concerns; they do not own the overall plan.

Brain reacts to persisted results and exceptions. Runtime delivers them to the
current Host and records delivery. No acknowledgement or resolve ceremony is
required: Brain decides whether to continue, accept, cancel or wait.

While a Work has a due Wake, Brain has no open polling turn. The
daemon's lifecycle timer wakes at the exact due instant or `claim_expires_at` and
re-enters normal event delivery. A daemon restart reconstructs that timer from
durable lifecycle state.

A discoverable external condition uses a source-specific wake or `due_retry`
with a durable `next_attempt_at`. The daemon timer creates one actionable wake
for one bounded check. Generic Brain-thread `user_input` cannot match that wait,
so unrelated conversation does not revise the Work or create a card.

The visible Brain timeline is a projection. One Work owns one replaceable card;
repeated delivery of its stable Event cannot append duplicate cards. Session,
provider, tmux, transcript, and process observations help decide whether an
Attempt is viable, but none can mark Work done.

Assistant replies are ingested from the current Host's provider transcript and
persisted in the Brain thread timeline for reconnect and history. A Codex native
thread switch can keep the same Host process alive: a single rollout open by
that process tree supersedes its saved transcript binding. If live evidence is
missing or ambiguous, the saved binding remains authoritative; cwd and latest
file timestamps never select a replacement. Rebinding preserves already
materialized history and does not replay user inputs.

Switching the Host executor keeps the current Brain thread and its durable
history. The App consumes the authoritative `brain_snapshot`, replaces the
Host subscription using its new Session ID and process metadata, and accepts
the new stream's snapshot before consuming its deltas. Draft ownership stays
with the logical Brain thread; late frames from the old subscription are ignored.

Transcript discovery can race a Host switch. Before publishing or persisting a
resolved transcript, the store must compare the complete Host binding with the
one that started the read. A departing provider cannot bind the newly selected
executor. For an already corrupted Claude/Codex binding, positive native header
evidence of a different provider invalidates the binding; missing or partial
files do not. A mismatched saved transcript must never replace the live Host's
conversation or hide its subsequent replies behind the prior Host's history.

Host continuity uses the daemon's fixed tmux socket. A negative cleanup probe
alone cannot replace a Host: a second successful inventory must confirm absence.
Unavailable transport and unknown provider identity preserve the binding and
block a new launch. Host discovery and launch are serialized within the daemon.
The shared Brain root also has a lifetime ownership lock, independent of the
authentication `--state-dir`; a second daemon using the same HOME must fail before
opening Brain state. Test daemons need a private HOME as well as private sockets
and state, so their fixtures cannot rewrite the user's Brain binding.

Host continuity runs once after initial watcher discovery, then only when the
watcher reports removal of the current Brain Host. It has no heartbeat probe,
retry timer, or separate health state. The existing transcript capture persists
the native conversation identity. Recovery resumes that exact provider Session
and broadcasts the replacement Host, preserving the logical Brain thread and
history. Unrelated or stale removal events cannot restart Brain. Recovery runs
outside the event broadcaster so provider startup cannot block watcher events.

A recovery failure is reported without a timer retry. Missing native identity
blocks blank replacement; recovery never selects the latest conversation by
directory or replays accepted user messages. Activation retains its existing
generation-specific receipt. Codex resume omits fresh-executor model and effort
defaults so the saved Session keeps its choices; persisted model-profile routes
retain their existing owner.

A recovered Host also needs its original model-proxy endpoint. On Linux,
daemon startup reads surviving Claude processes' structured `--settings` argv
and restores their literal loopback listeners only when the embedded opaque
route still belongs to the current route table. The existing Router serves both
the persisted address and any addresses retained by live clients; shutdown
closes them together. No provider input or process restart is needed. This
repairs listener metadata contaminated by an older secondary daemon while
preserving newer clients already using the recorded address. Verification must
check the Host's actual embedded route URL as well as its tmux/process binding.

Fresh Claude Hosts receive an explicit `--session-id`; resumes keep the recorded
native identity. Recovery can adopt an older Host with no resume argument using
Claude's `sessions/<pid>.json` record, its matching workspace, and the OS process
start identity (Linux). A stale PID record cannot prove ownership. Codex recovery
can use the live process's rollout or native thread identity. Workspace, hidden
status, and a Brain name restrict candidates but do not prove provider continuity
by themselves. A live candidate with unproven identity blocks duplicate launch.
Claude resume also checks the provider process registry when watcher inventory is
empty or points at a different socket. On platforms without a process-start
verifier, registry evidence fails closed; explicit command identity still supports
recovery. Activation receipts restore an already activated Host without replaying
its activation input.

Continue with `mewla worker send -id <session> --work-id <work> -text <follow-up>`.
Runtime mints the Turn token and atomically binds accepted input as execution.
There is no separate accepted-but-non-owning state or typed continue step.
Accept with `mewla brain work update -id <work> -status done`.
Worker reports and provider termination never implicitly accept Work.

An unknown send may have arrived. Brain decides whether to reconcile or retry;
a new attempt is permitted while the original unknown receipt remains evidence.
Source-write coordination is contextual, with isolation only when needed.
Exact receipts and stale-turn checks remain persistence correctness, not a
mandatory exclusive-writer product mechanism.

Delivered facts are not repeatedly admitted when a Host ends without resolving
them. They remain discoverable, explicit redelivery is possible, and independent
Work results proceed. New terminal evidence replaces an earlier provisional
exception and its current card; it cannot be stranded behind an old handling.
See [Work Lifecycle](../work-lifecycle.md) for responsibilities and transitions.

Fresh Brain homes receive the provider-neutral lifecycle and delegated
Worker protocol from the versioned templates under `daemon/brain/templates/`.
Managed-block repair refreshes those product-owned blocks while preserving
user-authored text and the private `profile.md`, `memory.md`, `current.md`, and
worklogs. `current.md` is the short active-work handoff, `profile.md` owns user
background and preferences, and `memory.md` owns durable facts and decisions.
Brain reads them on demand: `mewla brain context --json` and `brain_snapshot`
carry only their paths, sizes and budgets (16, 32 and 8 KiB), never their
contents, and `mewla brain gc` recommends compaction when a note exceeds its
budget. Communication principles live in the managed `AGENTS.md`; Mewla no longer
creates or reads `soul.md` and deletes an untouched shipped copy.

Brain Worklog boundary: internal audits, handoffs, and delegated reports belong
under the configured Brain workspace's `worklog/` directory (normally
`~/.mewla/brain/workspace/worklog`). They must not be written to a project
repository, a Worker cwd, or `cwd/docs/worklog`. Delegated reports should be
returned in the Worker result unless persistence is explicitly requested. Product
documentation is separate and must name its repository path explicitly.

The managed `AGENTS.md` asks for concise, direct prose in the user's language,
leading with the result, and a clear distinction between facts, assumptions and
recommendations. It does not impose a technical-writing standard or a fixed
response template. Prompt ownership and model guidance are in [prompting.md](prompting.md).
