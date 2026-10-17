# DSH Sessions

DSH is a native New Session launcher, not a normal Mewla Provider Chat or TUI. Codex
host and delegated defaults remain unchanged. Mewla supports the current upstream
release only (`@deepseek-ai/dsh` 0.2.1, installed with `npm i -g @deepseek-ai/dsh@next`).
It launches the installed `dsh` web profile on loopback behind an exact Session
bridge; it does not embed Cordis or the DSH web application in React Native. The
bridge runs `dsh --profile web --host 127.0.0.1 --port <port> --no-open` and adds a
private `--patch` that sets the persistence root and restates the `webserver` row on
loopback, so a user profile layer cannot widen the private listener. It does not
provide a DSH terminal UI. Mewla's small terminal surface is an input bridge
only, while the native RPC and persisted Session history remain authoritative.

When the registered `dsh-web.service` is active, Mewla exposes its server-projected
LAN or Tailscale URL as `Open Web`. No URL is fabricated for an absent, inactive,
loopback-only, or otherwise undiscovered service. The action uses the existing
authenticated service discovery and URL-opening path, so phones never receive a
localhost-only assumption.

The native `/api` requires the launch token `dsh web` prints with its URL. The bridge
reads it from its private Session log, exchanges it at `/?token=` for the signed
owner cookie, and calls Typert endpoints as `POST /api/<endpoint>` with
`payload.args.request` (`session/create`, `session/prompt`, `session/cancel`,
`session/modelCatalog`, `session/projections`, `session/selectModel`,
`session/attachment`). The bridge owns one native session ID, an exclusive lock and
a private Unix socket.
Native session logs live under the daemon state's `provider-sessions/dsh/logs`.
DSH retains ownership of its saved provider, model, reasoning and permission choices.
Mewla's Session Model control reads the native catalog plus the Session's
`modelSelection` projection and calls native `session/selectModel`; it does not rewrite `executors.toml`, DSH
settings or credentials. Selection acknowledgements report native asynchronous
persistence honestly. Existing Sessions are not migrated.

The conversation reader consumes the native session format v4 log
(`session.v4.jsonl.zstd`, header `version: 4`) and rejects other generations. Each
committed `assistant/message` carries its text and reasoning under a stable
turn/step identity; v4 persists no separate stream deltas, so text appears per
committed step. Tool results correlate by `message.toolCallId`. A turn that ends in
error shows its native error message as a failed status row. Plugin context stays out
of human chat. Native turn boundaries own Running, completion and interruption.
Persisted logs support history reload/reconnect without a phone URI. Native image
references resolve through their exact live Session and the existing generation
identity check. The common image viewer handles those results and ordinary file
references. Uploads become native images only from the authorized Mewla upload root;
DSH's native 5 MiB image-intake limit applies. Other files remain explicit attachments.

Stopping sends native `session/cancel`; closing the bridge disposes its native
runtime. Linux parent-death signals also stop that child after an abrupt bridge
exit. The private Session socket disappears on orderly shutdown. Resume uses the
same owned session ID and native log rather than creating a second conversation.

Native configuration must match the installed DSH version. Startup failures are
reported in the bridge's private Session log; Mewla never repairs credentials or
chooses a different provider automatically. DSH permission and question requests
appear in Interface's review sheet. The bridge holds the native forwarded-event
stream (`$events` over the `/api/remote.mux` WebSocket) and receives
`approval/request` and `user-questions/request` waterfalls; other waterfalls pass to
the next listener. Native event IDs, the stream's client ID and connection epochs
fence every answer. Allow once, Reject, structured option/custom answers, and
cancellation post `$events/result`; cancelled, replaced, or disconnected requests
cannot be answered from stale UI. Native permissions remain fail closed.
Native terminal output is a minimal input bridge; the shared Interface provides
message, reasoning, tool and image presentation. Windows daemon hosts do not expose
the Unix Session bridge. Android and iOS use the same mobile implementation.
