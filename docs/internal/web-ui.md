# Web UI

The daemon serves the Mewla client as a web page. It is the same TSX app as Android and
iOS (`client/ui`, built with `nsh package --target web`), with the same Brain, Sessions, Terminal, Work, and Settings screens. A
browser pairs as its own trusted device, so it has the same authority as a
paired phone and you can revoke it the same way.

## Open it on this computer

```bash
mewla pair
```

This asks the running daemon for a one-time pairing token and prints, among the
phone QR, a **Browser on this computer** link,
`http://127.0.0.1:9876/#pair=...`. Open it in your browser. The page asks **Pair
this browser?**; confirm **Pair and grant access** and the browser lands in
Settings, connected. Later visits to `http://127.0.0.1:9876/` reuse that
pairing.

A Session's address, `/terminal/<id>`, opens that Session when you reload it or
paste it into a new tab. Without `?serverId=` it means the current server. A
link to a closed or unknown Session, or to another server, opens the Session
list.

- The link works once and expires after 15 minutes. Do not share or paste it
  anywhere else. `mewla pair` never opens a browser itself.
- The loopback link uses the port the daemon listens on (`-addr`).

The pairing link is carried in the URL fragment. Browsers never send the
fragment to the server, and the app removes it from the address bar once it has
read it.

You can also paste a `mewla://` pairing link in Settings. A browser pairs only
with the daemon that serves the page. A link for the same daemon at another
address, such as its tunnel hostname, pairs and connects through the page you
are on. A link for a different daemon shows an error: open that daemon's own
web UI or run `mewla pair` on that computer and open its browser link.

## Open it from another device

Remote access is off by default. On the default bind, the daemon serves the
page only to loopback clients that use a loopback `Host`. Requests from any
other address or hostname get 404, including DNS-rebinding attempts. `mewla --lan`
does not expose the web UI over plain HTTP.

To use it remotely, put the daemon behind an HTTPS endpoint the browser trusts,
such as Tailscale Serve, Cloudflare Tunnel, or a reverse proxy. The endpoint
must forward the full origin (see [Connect and pair](../connect-and-pair.md)) and
keep the original `Host` header. Then add that exact address:

```bash
mewla address add https://mewla.example.com
mewla pair
```

Open the printed **Browser anywhere** link on the remote device. The web UI is
served at every `https` address in `mewla address list` (added by hand, or seen
on a paired device's signed request), live, without a restart. Every API call
still requires a paired device signature. The address book only decides which
hostnames may load the page.

## Revoke a browser

Clearing site data in the browser forgets the pairing locally, but the daemon
still trusts that key. To revoke it on the daemon:

```bash
mewla devices list          # browser devices are named "Mewla Web (...)"
mewla devices revoke -id <device-id>
```

## Differences from the mobile apps

Brain and Sessions keep their mounted state when switching tabs. A browser
resets a hidden list's scroll, so a chat given `shown` (`client/ui/chat/Timeline.tsx`)
records its offset while visible and restores it, or the newest row, when its tab
comes back.

| Feature                          | Web                                                     |
| -------------------------------- | ------------------------------------------------------- |
| Brain, Sessions, Work, Settings  | Same as mobile                                          |
| Terminal                         | xterm.js renderer with the same terminal protocol       |
| Pairing                          | `mewla pair` browser link or paste a `mewla://` link; no QR camera scan |
| Mewla Relay (pinned TLS) | Unsupported; browsers cannot pin the daemon certificate |
| Push notifications               | Unsupported                                             |
| File upload from the device      | Unsupported                                             |
| In-app Browser and PDF preview   | Unsupported                                             |

The browser stores its device key in local storage for the page origin. That is
weaker than the platform keychain the mobile apps use, so only pair browser
profiles you control.

## Building

Check the TSX with `cd client/ui && nsh check`, run the screens with
`nsh test`, and the shared logic with `cd client && bun test core`. A page is
reachable by its address: a Session at `/terminal/<id>`, and the app restores
the place and the current server from the browser history entry
(`client/ui/shell/webLocation.ts`).

Release and CI daemon builds embed the web build. A source build without it
serves a short message instead of the app. To embed it locally:

```bash
bun install
./scripts/build-web-ui.sh   # or: bun run web:build
cd daemon && go build ./cmd/mewla
```

For development, `cd daemon && go run ./cmd/mewla-dev` builds the current web
sources before its first daemon build. It watches `client/ui`, `client/core`,
`scripts/build-web-ui.sh` and daemon sources. Client changes rebuild the web UI
before rebuilding the daemon; generated bundles and dependencies do not trigger
another rebuild. A failed web build keeps the previous daemon running.

An already running dev watcher keeps its own compiled code until its next launch.
When changing the watcher itself, stage the web bundle with `bun run web:build`
before the next normal Go source reload. Never assume a Go-only reload refreshed
the embedded app. An obsolete web operation now asks the person to reload or
update Mewla instead of claiming that an account is missing.
