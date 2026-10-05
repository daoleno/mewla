# Web UI

The daemon serves the Zen app as a web page. It is the same app as Android and
iOS, with the same Brain, Sessions, Terminal, Work, and Settings screens. A
browser pairs as its own trusted device, so it has the same authority as a
paired phone and you can revoke it the same way.

## Open it on this computer

```bash
zen web
```

This asks the running daemon for a one-time pairing token. It then opens
`http://127.0.0.1:9876/#pair=...` in your default browser. Confirm **Pair and
grant access** and the browser lands in Settings, connected. Later visits to
`http://127.0.0.1:9876/` reuse that pairing.

- `zen web -no-open` prints the link instead of opening it. The link works once
  and expires after 15 minutes. Do not share or paste it anywhere else.
- `zen web -origin http://localhost:9876` uses a different loopback address.
  The origin must be the address the browser loads the page from.

The pairing link is carried in the URL fragment. Browsers never send the
fragment to the server, and the app removes it from the address bar once it has
read it.

## Open it from another device

Remote access is off by default. On the default bind, the daemon serves the
page only to loopback clients that use a loopback `Host`. Requests from any
other address or hostname get 404, including DNS-rebinding attempts. `zen --lan`
does not expose the web UI over plain HTTP.

To use it remotely, put the daemon behind an HTTPS endpoint the browser trusts,
such as Tailscale Serve, Cloudflare Tunnel, or a reverse proxy. The endpoint
must forward the full origin (see [Connect and pair](connect-and-pair.md)) and
keep the original `Host` header. Then allow that exact origin:

```bash
zen -web-origin https://zen.example.com
# in another terminal, on the daemon host:
zen web -origin https://zen.example.com -no-open
```

Open the printed link on the remote device. `-web-origin` accepts only `https`
origins without a path, and you can repeat it. Every API call still requires a
paired device signature. The flag only decides which hostnames may load the page.

## Revoke a browser

Clearing site data in the browser forgets the pairing locally, but the daemon
still trusts that key. To revoke it on the daemon:

```bash
zen devices list          # browser devices are named "Zen Web (...)"
zen devices revoke -id <device-id>
```

## Differences from the mobile apps

| Feature                          | Web                                                     |
| -------------------------------- | ------------------------------------------------------- |
| Brain, Sessions, Work, Settings  | Same as mobile                                          |
| Terminal                         | xterm.js renderer with the same terminal protocol       |
| Pairing                          | `zen web` or paste a `zen://` link; no QR camera scan   |
| Zen Link (relay with pinned TLS) | Unsupported; browsers cannot pin the daemon certificate |
| Push notifications               | Unsupported                                             |
| Mermaid diagrams                 | Shown as source                                         |
| File upload from the device      | Unsupported                                             |
| In-app Browser and PDF preview   | Unsupported                                             |

The browser stores its device key in local storage for the page origin. That is
weaker than the platform keychain the mobile apps use, so only pair browser
profiles you control.

## Building

Release and CI daemon builds embed the web export. A source build without it
serves a short message instead of the app. To embed it locally:

```bash
bun install
./scripts/build-web-ui.sh   # or: bun run web:build
cd daemon && go build ./cmd/zen
```
