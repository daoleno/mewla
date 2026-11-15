# Worker Session identity

A Worker owns one Session in the session host, named by the id the host gave
it at creation (`mh-` and 24 hex digits). Discovery, status, process identity,
input serialization, capture, receipts and cleanup all use that id. The
Session's name is display metadata. If the Session disappears, the Worker is
gone; there is no name or position fallback.

Mewla keeps ownership, delegated-resource and Pi transcript markers as the
Session's labels in the host (`origin`, `delegated`, `resource-unit`,
`pi-session`, …), set when the Session is created. Receipt ledgers are keyed by
the Session id. Cleanup closes that Session only.

The launch prologue derives `MEWLA_WORKER_ID` from the `MEWLA_SESSION_ID` the
host sets. A Session never inherits `TMUX` or `TMUX_PANE` from the daemon's
environment, and Worker and Brain Sessions get a private `TMUX_TMPDIR`, so a
`tmux` an agent runs starts a server of its own instead of reaching the
person's.

## Verification

`daemon/watcher` covers ownership, absence and cleanup against a fake backend,
and the `ghosttyvt`-tagged tests (`scripts/test-ghosttyvt.sh`) run them against
a real session host. The same daemon contract serves Android and iOS. No mobile
route or client-side server selection is introduced.
