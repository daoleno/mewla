# Worker pane identity

A Worker owns one immutable tmux `%pane_id` on the daemon's canonical tmux
server. Creation returns that pane ID; discovery, status, process identity,
input serialization, capture, receipts and cleanup all use it. Window names and
session names are display and grouping metadata. Splitting a window, selecting
another pane, moving the owned pane to another window, or linking a Terminal
view does not change the Worker. If its pane disappears, the Worker is gone.
There is no window-name or active-pane fallback.

Mewla stores ownership, delegated-resource and Pi transcript markers with
`set-option -p`. Receipt ledgers are pane-local too. An unowned sibling cannot
inherit ownership from window, session or global options. Cleanup uses
`kill-pane`, so user panes in the same window survive.

Creation allocates an inert pane, then launches its command with
`respawn-pane -e` before publishing the Worker. This gives environment values to
the owned pane without `new-session -e` leaking them into later user windows or
splits, and keeps environment secrets out of `pane_start_command`. The launch
prologue derives `MEWLA_WORKER_ID` directly from `TMUX_PANE`. Tmux client startup also strips Worker and Brain launch context so a newly
started default server cannot publish it as global environment. Provider-internal
tmux isolation and process-generation checks remain in place.

## Pre-pane Workers

Workers created before pane identity are not migrated. Their windows carry no
pane ownership marker, so discovery ignores them, and the control API no longer
translates an old `session:window_id` Worker ID to a pane.

## Verification

The isolated real-tmux regression creates a Worker, focuses a newly split user
pane and submits through the real input owner. Only the Worker receives the
payload. Removing it makes the next submission `not_submitted` and preserves
the sibling. Additional scratch tests cover environment isolation, pane-local
ownership, cleanup, distinct Workers in one window, and Terminal window
linkage.

The same daemon contract serves Android and iOS. Terminal still presents the
containing window for manual interaction; automatic Worker input always targets
the owned pane. No mobile route or client-side server selection is introduced.
