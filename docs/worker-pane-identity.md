# Worker pane identity

A Worker owns one immutable tmux `%pane_id` on the daemon's canonical tmux
server. Creation returns that pane ID; discovery, status, process identity,
input serialization, capture, receipts and cleanup all use it. Window names and
session names are display and grouping metadata. Splitting a window, selecting
another pane, moving the owned pane to another window, or linking a Terminal
view does not change the Worker. If its pane disappears, the Worker is gone.
There is no window-name or active-pane fallback.

Zen stores ownership, delegated-resource and Pi transcript markers with
`set-option -p`. Receipt ledgers are pane-local too. An unowned sibling cannot
inherit ownership from window, session or global options. Cleanup uses
`kill-pane`, so user panes in the same window survive.

Creation allocates an inert pane, then launches its command with
`respawn-pane -e` before publishing the Worker. This gives environment values to
the owned pane without `new-session -e` leaking them into later user windows or
splits, and keeps environment secrets out of `pane_start_command`. The launch
prologue derives `ZEN_WORKER_ID` directly from `TMUX_PANE`. Tmux client startup also strips Worker and Brain launch context so a newly
started default server cannot publish it as global environment. Provider-internal
tmux isolation and process-generation checks remain in place.

## Existing Workers

At daemon startup, before the control server, polling or Brain lifecycle engine
starts, Zen migrates legacy `session:window_id` references. It inventories exact
window and pane IDs and matches recorded pane-generation evidence. A legacy
window with no such evidence can migrate only if it has one pane. An ambiguous
split stops startup with an explicit identity error; focus is never evidence.

The private `state/worker_pane_aliases.json` journal commits the chosen pane
before any tmux metadata or state document changes. Each state document is
atomically replaced, and startup repeats the migration safely after an
interruption. Lifecycle rows and events, presentation references, Host binding
and activation, timeline session references, provider route bindings (including launch/history
bindings), and Telegram reply/topic routes use the pane ID afterward. Telegram
delivery checkpoints migrate with the routes to preserve message deduplication.
Turn tokens, receipts, process/pane generations, provider transcript identities
and message text are preserved. Existing running shells may continue reporting
the old `ZEN_WORKER_ID`; the control API accepts that saved alias only when the
pinned pane still carries its exact legacy marker.

Migration moves legacy ownership and receipt options off windows and removes
Zen context, private scratch and provider configuration-root variables from
legacy Zen-created session environments. It does not send input or relaunch
running providers.

## Verification

The isolated real-tmux regression creates a Worker, focuses a newly split user
pane and submits through the real input owner. Only the Worker receives the
payload. Removing it makes the next submission `not_submitted` and preserves
the sibling. Additional scratch tests cover environment isolation, pane-local
ownership, cleanup, migration with focus on a sibling, ambiguous migration,
repeated store reopening with a running turn, and Terminal window linkage.

The same daemon contract serves Android and iOS. Terminal still presents the
containing window for manual interaction; automatic Worker input always targets
the owned pane. No mobile route or client-side server selection is introduced.
