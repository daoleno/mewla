# Architecture

Mewla is a mobile-native control plane for coding agents that continue to run on
the user's computer. The daemon remains the business-logic and data owner.
Self-managed Pair/direct connectivity is the normal path. Explicitly configured
Mewla Relay adds an optional reachability path; it does not move Sessions,
Terminal, Brain, Chat, Calendar, Work, files, or credentials into the relay.

## System shape

```text
                          operator-only HTTPS
                          /healthz /readyz /metrics
                                      │
[Android / iOS app]                   │
  shared StoredServer owner           │
  native SPKI-pinned TLS              │
          │                           │
          │ inner TLS 1.3 carrying HTTP/1.1 + WSS
          ▼                           ▼
┌───────────────── Regional Mewla Relay ──────────────────┐
│ bounded TLS ClientHello SNI routing only                │
│ opaque bidirectional L4 streams; no inner TLS keys      │
│ in-memory route presence and one-time admissions        │
└───────────────┬─────────────────────────────────────────┘
                │ outer TLS 1.3, daemon-initiated only
                │ one control connection + one outbound data
                │ connection per mobile stream
                ▼
        [daemon Relay Connector]
                │ terminates the pinned inner TLS
                ▼
        [the existing daemon HTTP handler]
 /ws /pair /auth-check /upload /session-file-capability /session-file /health /devices
                │
                ▼
 [session host, agent CLIs, repositories, daemon-local state]
```

LAN, Tailscale, Cloudflare Tunnel, and reverse-proxy origins continue to call
the exact same daemon HTTP handler without Mewla Relay. The daemon opens no new
public listener for Mewla Relay.

The same handler also serves the client's web build (`daemon/webui`, built by `scripts/build-web-ui.sh`) to loopback
and to the HTTPS addresses in the daemon's address book (`mewla address`). A
browser pairs as one more device and uses the same signed requests; see
[In a browser](connect-and-pair.md#in-a-browser).

## Durable invariants

1. Exactly one `StoredServer` is current. Relay regions and future direct paths
   are transport candidates of that record, never extra servers and never a
   feature-local server picker.
2. The daemon is the sole application endpoint. Relay loss cannot make a
   self-managed/LAN transport depend on the relay.
3. The relay has no inner TLS private key and cannot read HTTP paths, headers,
   device signatures, WebSocket messages, Terminal bytes, prompts, or files.
4. Existing Ed25519 daemon identity and enrolled device keys remain the
   application trust anchors. A separate Relay TLS key is explicitly signed by
   the daemon identity in Pairing V2.
5. Pairing admissions, enrollment tokens, stream tickets, auth nonces, and
   device signatures are purpose-bound and replay resistant.
6. Android and iOS consume the same TypeScript transport contract. Only the
   TLS/SPKI socket bridge is platform native.

## Execution Ownership

Brain owns objectives, scheduling, review and acceptance. A Mewla Worker is its
managed execution unit, distinct from provider-native Agent roles and Codex
internal subagents. The lifecycle engine serializes Work transitions; Watcher
supplies exact Session/process evidence and publishes ordered projections.
Host transport receipts and Worker execution authority are separate records.
See [Work lifecycle](work-lifecycle.md) for receipt and upgrade contracts.

Watcher takes an inventory epoch before external reads. A registration or
control update after that boundary cannot be deleted or overwritten by the old
inventory. Missing-Session evidence is persisted outside the state lock and
retried on storage failure. Event publication is serialized separately, so a
slow event consumer can read a snapshot without deadlocking the publisher.

The app's current-server owner also guards delayed results. Sessions, service
discovery, Stats, Terminal and Work routes do not choose a server by latency or
aggregate configuration records. If the persisted current server is no longer
paired, the app has no current server and shows its no-server state; it does not
fall back to the first paired computer (only a phone that never stored a current
server picks the first one, once). Removing the computer in use from its settings
page moves to the next remaining computer as that action's explicit choice.
Switching servers hides old data immediately;
already-submitted operations retain their original identity until settlement.
Unsaved Work text remains in the app-lifetime Work draft store, keyed by server
and document. This is not durable offline storage; daemon saves remain the
durable document authority.

## Mewla Relay protocol

The MVP deliberately uses standard, widely implemented primitives:

| Boundary                       | Protocol                                                       | Reason                                                                                        |
| ------------------------------ | -------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| Phone to daemon, through relay | TLS 1.3 with X.509 Ed25519 certificate and Pairing V2 SPKI pin | True end-to-end confidentiality/authentication; relay has no inner key                        |
| Direct address to a hosted computer | TLS 1.2 or 1.3 with the computer's WebPKI certificate, only for its `*.m.mewla.ai` hostname | An iPhone's WebSocket stack cannot speak TLS 1.3; forward-secret AEAD suites only. The pinned path stays TLS 1.3 |
| Inner application stream       | HTTP/1.1, WebSocket Upgrade, Range and streaming bodies        | Preserves every current full-origin route without an application rewrite                      |
| Connector control/data         | TLS 1.3 TCP, ALPN/frame protocol version 2                     | Length-bounded control, atomic pairing-admission commit, and explicit mixed-version failure   |
| Relay routing                  | bounded TLS ClientHello SNI inspection                         | Routes before forwarding while preserving the complete inner ClientHello                      |
| Each client stream             | a fresh daemon-outbound connector data connection              | Natural stream isolation and backpressure; large upload cannot block Terminal in a shared mux |

The relay does not terminate the inner TLS. Its stable SNI label is a random
128-bit route ID. Pairing uses a separate random 128-bit admission alias that
expires. A client TLS connection first reserves the alias; an empty TLS
preflight or a non-`POST /pair` request releases that reservation. The
Connector commits consumption through a daemon-signed
route+alias+stream control message only after it receives the actual pairing
request. The relay performs that commit atomically, so concurrent use and
post-pair replay still fail without the relay reading inner HTTP. Once routed,
the relay sends the connector a random stream ID and 256-bit single-use
attachment ticket.

Connector registration and admission requests carry:

- protocol version
- random route and nonce
- daemon ID and public key
- short timestamp
- operator-provisioned connector admission token
- an Ed25519 daemon signature over a domain-separated canonical frame

The operator token is an MVP service-admission control, not application trust.
It is never sent to the phone and cannot impersonate a daemon or decrypt a
stream. There is intentionally no user-account database or global control plane
in this slice.

### Backpressure and isolation

Relay copies use fixed 32 KiB buffers, one shared last-progress clock for both
directions, socket read/write deadlines, bounded client/route/handshake
concurrency, and no unbounded stream queue. Only a stream with no progress in
either direction reaches the idle timeout. A one-sided EOF propagates
`CloseWrite` and leaves the reverse direction open to drain a delayed response.
TCP backpressure reaches the sender. Each HTTP or WebSocket connection has its
own outer connector socket, so a large `/upload` body is independent of a live
Terminal WebSocket. Existing daemon uploads are HTTP, while Terminal remains on
`/ws`; Relay preserves that separation.

Inside the daemon, each `/ws` client has its own ordered send queue and a single
writer goroutine. Broadcasts, request replies and watcher events only enqueue,
so a client that stops reading (a suspended phone, a dead proxy leg) never
stalls broadcast, the watcher, control-socket writes or session creation. The
writer sends each message in 32 KiB chunks and renews the 30 s write deadline
per chunk, so the deadline measures progress: a slow link that keeps draining
is never cut off mid-snapshot, while a client that accepts nothing for 30 s or
lets its queue pass 16 MiB is closed and reconnects to fresh snapshots. JSON
messages use permessage-deflate when the client offers it. A client whose
WebSocket cannot negotiate that (the apps, on NSH's WebSocket) offers the
subprotocol `mewla.frames.deflate-raw` beside its auth subprotocol; the writer
then sends each JSON message of 1 KiB or more as one binary message of raw
DEFLATE (RFC 1951), and smaller ones as text. The handshake still selects the
auth subprotocol, and a client that does not offer it gets text. The
connection-lost log line adds `frames=deflate-raw wire=<bytes>` for such a
client.

A thread's first snapshot can come in two parts. A
`codex_conversation_subscribe` with `first_events: N` gets a
`codex_conversation_snapshot` of the newest N events, then a
`codex_conversation_delta` (the next revision) with every older event, so a
phone on a slow link paints the end of the thread from a few KB while the
rest follows. Without it, or for a thread of N events or fewer, the snapshot
is the whole thread.

What goes first on a slow link: a connection starts with `brain_snapshot`
(an app opens on Brain), then the Session list. Work, Calendar and pictures
(`work_item*`, `calendar_item*`, `codex_asset`) wait in a bulk lane while
anything else is queued, in order among themselves, until 256 KiB of other
messages went out past a waiting one. The Work and Calendar snapshots also
wait for the connection's first `codex_conversation_snapshot` (at most 1.5 s),
so the thread an app opens is not behind 300 KB of them. Each client socket
sets TCP_NOTSENT_LOWAT (16 KiB) where the platform has it, so the backlog
stays in that queue, where this order applies, instead of megabytes of it in
the kernel. A `codex_asset` with `max_edge: N` answers the picture scaled down
to N pixels on its long side and re-encoded (JPEG when opaque, else PNG) when
that is smaller; GIFs stay as they are. The apps ask 1600 for a thread's row
and the original for the full-size viewer.

A client that offers the subprotocol `mewla.sessions.tailless` beside its auth
subprotocol (the NSH apps since v0.4.3, which never show a Session's terminal
tail) gets `worker_session_list` and every `worker_session_*` change without
`last_output_lines`, in the same seq as everyone else. A running Session's tail
was most of each update (8-16 KB of terminal text every 10 s per Session). A
client that does not offer it gets the tail as before.

A message of 64 KiB or more on the wire (after frame deflate) is announced
first by `{"type":"frame_coming","bytes":N}`, its size on the wire. The apps
count a socket that carried no message for 25 s as dead and see nothing of a
message until all of it is in; told the size, they allow for it at 8 KB/s on
top of the 25 s, until the next message arrives (a full-size picture of 3 MB
takes about a minute at 60 KB/s). A client that does not know the type
ignores it.

The NSH host hands the app's JS the WebSocket messages that came in together
in one call (9 to 36 in a row, measured). The app hands each turn's messages
to its state on the next tick inside one `batch`, so what reads them (the
tray, the Session list) is worked out once a turn, not once a message.

Connect snapshots stay small as history grows: `calendar_items_snapshot`,
`calendar_item_changed` and Calendar mutation replies omit run history
(`get_calendar_item` returns it), `brain_snapshot` carries no scheduled-result
bodies (they reach Brain through its thread conversation), and
`work_items_snapshot` lists only calendar Work that is unfinished, linked from
a Calendar item or tied to a live Session, each with its full body. Terminal output for a client waits
for room in that client's queue instead, so a busy terminal slows to the
client's pace. The watcher publishes events through an ordered overflow queue
and never blocks on its consumer.

The MVP does not add application-level resume to protocols that do not already
have it:

- WebSocket reconnect re-authenticates and the app refreshes canonical current
  server snapshots.
- The Session list is a sequenced feed. The daemon sends the full
  `worker_session_list` once per connection, stamped with the `seq` it is
  current as of, and every later `worker_session_created`, `_updated` or
  `_archived` carries the next `seq`. The app applies a change only when its
  `seq` is one past the last, drops duplicates, and on a gap asks once for a
  fresh list (`list_worker_sessions`); there is no periodic full list. Every
  10 s the daemon sends a `worker_session_heartbeat` with the current `seq`
  and no Session: it keeps an idle socket from being judged silent, and a
  `seq` ahead of the app's last means a change was lost, which asks for a
  fresh list. A change that is only a Session's terminal output (its
  `last_output_lines`, which the apps since v0.4.0 do not show) is sent at most
  once every 10 s per Session, newest last; state and metadata changes are
  never held. A daemon check once a minute publishes, as ordinary sequenced changes, any
  Session whose derived fields (capabilities, executor, provider) moved
  without a watcher event. Absence from a list is never proof of anything:
  `kill_worker` is answered with `worker_killed` (or `error`), which is what a
  terminate waits for. The app and daemon change this together; an app that
  predates it must update.
- HTTP Range lets Session File Preview resume/read bounded byte ranges. The app
  spends a fresh ordinary nonce once on `/session-file-capability`, then native
  GET/HEAD/Range and retry use a two-minute daemon-signed capability bound to
  device, live Session/process/start, path, generation, and HTTP method.
  Video and audio stream through the same route: the daemon names them
  `video`/`audio` only when the extension and the container signature agree,
  serves `http.ServeContent` Range responses straight from the open file (never
  reading it whole) under a 2 GiB media bound (images and PDFs keep 50 MiB),
  and the player signs a fresh capability for the same generation when a seek
  after the two minutes is refused, resuming where it was.
- Upload retry restarts the request; the server does not claim resumable upload.
- Terminal latency comes from an independent stream, not priority scheduling in
  a custom multiplexor.

## Pairing versions

### Pairing V1 — preserved

`mewla pair` without Relay, or `mewla pair <address>`, emits the compact binary V1
payload for the chosen address (never loopback when chosen automatically). It includes
the full phone-reachable `/ws` URL, daemon public key, and one-time enrollment
token. HTTP(S) is normalized to WS(S); the app derives the other root routes.
This is the contract for LAN, Tailscale, Cloudflare Tunnel, and reverse proxies.

### Pairing V2 — Mewla Relay

When `relay.json` exists, `mewla pair` with no address requests a short-lived
relay admission and emits a JSON V2 payload inside URL-safe base64. It includes:

- daemon ID and public key
- one-time daemon enrollment token and expiration
- stable unenumerable route ID
- relay candidate names, one admission URL, and stable URLs
- Relay transport SPKI SHA-256 pin
- daemon Ed25519 signature over every field above

The app verifies this binding before opening the pinned transport. For the
admission URL it starts only the loopback listener; it does not open a remote
TLS preflight. The actual `POST /pair` creates the first admission-bearing
stream. After enrollment it stores stable relay candidates on the same
`StoredServer`. Re-importing Relay for the same daemon updates/reuses that server
record instead of creating a region-specific duplicate.

## Request authentication and revocation

After enrollment, HTTP and WebSocket operations keep the current `Device`
signature contract. Signatures bind daemon ID, device ID, purpose, timestamp,
and nonce. `/auth-check` and the following `/ws` probe are distinct requests
and therefore each build a fresh timestamp, nonce, and signature. Session File
capability issuance also spends one fresh ordinary nonce; the resulting
short-lived GET/HEAD signatures are deliberately repeatable for native Range
recovery and are rejected after expiry, device revocation, or any bound-field
change. Current purposes cover connection, probe, upload, Session File, and
device administration. Daemon response assertions still prove the paired
daemon on Pair, Health, and probes.

`mewla devices list` and `mewla devices revoke -id <device-id>` provide the minimum
host owner for revocation. When the daemon is running, revoke goes through its
mode-0600 local control socket so an in-flight authorization update cannot
resurrect a stale key; offline revoke updates the same state directly.
Authenticated `GET /devices` and `DELETE /devices` provide the network protocol
owner. Revocation removes the key from `trusted-devices.json`; subsequent signed
requests fail immediately.

## Relay selection and failure

The daemon tries configured candidates, measures completed control TLS plus
registration RTT, deterministically selects lowest RTT (name/address breaks a
tie), closes unused registrations, and keeps one primary control connection.
On loss it performs deterministic bounded exponential reconnect and remeasures.
A route conflict is explicit and never silently replaces an existing daemon.

The app probes all stable candidates through the same native pin owner, chooses
the lowest successful RTT deterministically, and caches that selection briefly.
If the connector has failed over to another configured relay, the next mobile
selection reaches the new route. Manual candidates are not silently mixed into
Relay failover; the user selects a self-managed record/path explicitly.

Route presence is in memory. A relay restart deliberately disconnects routes;
connectors re-register. A single-region MVP therefore runs one relay process
(or one active replica). Horizontal scale requires a later minimal route-owner
directory or deterministic connection sharding so a client reaches the same
process as its connector. This repository does not pretend that a load
balancer alone solves that state placement.

## Region model

The shipped unit is region-neutral. Each candidate declares:

- connector control address and certificate name
- client wildcard domain and port
- stable operator name

A production operator can place one active relay in each selected region and
use GeoDNS/Anycast only to steer a region-specific hostname to that region.
The candidate hostname remains explicit in Pairing V2, so failover does not
depend on hidden DNS substitution. Health checks use `/healthz`; traffic
readiness uses `/readyz`. Connector registrations are not a readiness
requirement because an empty relay must still accept new routes.

Initial service objectives to validate before any production claim:

- relay process availability: 99.9% monthly per region
- successful registered-route connection setup: p95 ≤ 1 s in-region, p99 ≤ 3 s
- relay-added steady-state Terminal RTT: p95 ≤ 25 ms within the selected region
  (measured against a direct baseline)
- reconnect after process restart: p95 ≤ 10 s, p99 ≤ 35 s
- wrong pin, expired/replayed admission, and unknown route acceptance: zero
- metadata/content leakage in relay logs and metrics: zero

These are acceptance targets, not claims that a production network exists.

## Protocol choices intentionally deferred

| Candidate             | MVP decision                                                                          |
| --------------------- | ------------------------------------------------------------------------------------- |
| WebSocket over TLS    | Keep as the inner real-time application protocol                                      |
| HTTP/2 CONNECT        | Not needed; mobile and daemon already share a full HTTP/WSS origin                    |
| HTTP/3 / WebTransport | Prototype later only if cellular loss/hand-off data proves benefit                    |
| custom QUIC mux       | Not in MVP; would duplicate congestion, resume, and mobile work                       |
| WireGuard / DERP      | Not selected; would turn Mewla Relay into a device VPN/overlay and expand platform/ACL scope |
| ICE / STUN / TURN     | Future direct-path optimization only; NAT traversal must not block relay MVP          |
| P2P hole punching     | Explicitly out of scope for this slice                                                |

## Storage

Daemon state adds:

- `<state>/relay.json` — optional operator connector configuration; may name an
  environment variable instead of embedding its token
- `<state>/link-identity.json` — stable random route and Relay Ed25519 transport
  private key, mode `0600`

Existing identity, pairing token, trusted-device, upload, Work, Brain, Calendar,
and agent state remain owned by the daemon. The relay has no database and
persists none of them.

The app keeps its Ed25519 device seed in secure storage. The app's key-value
storage (`client/core/servers.ts`) keeps one
server record with daemon trust plus optional Relay route, SPKI pin, and
transport candidates.

## Capacity and cost drivers

Relay capacity is dominated by concurrent TCP/TLS sockets, file-transfer
egress, kernel buffers, and encryption for the outer connector TLS. Inner
encryption happens on phone and daemon. Metadata metrics expose only aggregate
active routes/streams, accepted/rejected connection counts, and total forwarded
bytes. Route IDs, SNI, daemon IDs, filenames, paths, request headers, tokens,
and content must never be metric labels or logs.

The current daemon upload policy is **2 GiB per file**, **8 GiB aggregate
stored uploads**, and seven-day retention. Tests use bounded readers and small
fixtures; no multi-GiB artifact belongs in the repository.
