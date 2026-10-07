# Connect and pair

Pairing connects the app on your phone to the daemon on your computer. You need
two things: a network path from the phone to the daemon, and a one-time pairing
code.

The daemon has its own Ed25519 key. During pairing the phone enrolls its own
key once with a short-lived code; after that it signs every request. There is
no shared password.

## Choose a route

| Route | Use it when | Start the daemon with |
| --- | --- | --- |
| [Same Wi-Fi](#same-wi-fi) | Phone and computer share a trusted network | `mewla --lan` |
| [Tailscale](#tailscale) | You want private access from anywhere | `mewla -addr "$(tailscale ip -4):9876"` |
| [Cloudflare Tunnel or a reverse proxy](#cloudflare-tunnel-or-a-reverse-proxy) | You want a stable HTTPS address without a VPN app | `mewla` |

The first time the daemon starts with no paired device, it prints a pairing QR
code and link. For later devices, use `mewla pair` or
[approve the device from a phone you already paired](#add-another-device).

## Same Wi-Fi

1. Start Mewla in private-network mode:

   ```sh
   mewla --lan
   ```

2. Mewla prints a complete `mewla pair` command for each private address it
   finds. Run the **Same Wi-Fi/LAN** one in another terminal. Never pair
   with `0.0.0.0`.
3. Scan the QR code or import the link in the app.

The phone must reach the computer on port `9876`; a host firewall or Wi-Fi
client isolation can block it. LAN traffic is plain HTTP, so use this route
only on a network you trust. `mewla --lan` listens on every IPv4 interface.

## Tailscale

Use [Tailscale](https://tailscale.com/docs/install) when the phone should
connect privately from cellular data or another network.

1. Install Tailscale on the computer and the phone, sign both into the same
   tailnet, and make sure your tailnet's access rules let the phone reach the
   computer.
2. Bind Mewla to the computer's Tailscale address only:

   ```sh
   mewla -addr "$(tailscale ip -4):9876"
   ```

3. Check that the phone can open `/health` at the address Mewla printed, then run
   the matching `mewla pair` command in another terminal and scan the result.

Traffic stays inside Tailscale's encrypted network. Binding the Tailscale
address keeps port `9876` closed on your local network.

## Cloudflare Tunnel or a reverse proxy

Use this when you want a stable HTTPS address and do not want a VPN app on the
phone. With Cloudflare, the domain must be on your Cloudflare account.

1. Keep Mewla on its default loopback address:

   ```sh
   mewla
   ```

2. Follow Cloudflare's
   [Create a tunnel (dashboard)](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel/)
   guide: create a named tunnel, run the `cloudflared` command it shows on the
   Mewla computer, and add a **Published application** route whose service URL
   is `http://127.0.0.1:9876`.
3. Do not add a path restriction. Mewla needs the whole origin, including `/ws`,
   `/health`, `/auth-check`, `/pair`, `/upload`, `/session-file-capability`,
   `/session-file` and `/devices`. Path-prefixed origins such as
   `https://example.com/mewla` are not supported.
4. Check that the phone can open `https://mewla.example.com/health` (with your
   real hostname), then:

   ```sh
   mewla pair https://mewla.example.com
   ```

Any other HTTPS reverse proxy works the same way if it forwards the full origin
and WebSocket upgrades to `http://127.0.0.1:9876`.

The published hostname is reachable from the internet unless you add an access
layer. `/health` answers without authentication; everything else needs a
pairing code or a paired device's signature. A Cloudflare Access login page is
not supported by the app. Keep pairing links private.

## Generate a pairing link

With the daemon running:

```sh
mewla pair https://mewla.example.com   # an origin the phone can reach
mewla pair                           # use the daemon's known addresses
```

Pass the scheme and host the phone actually reaches. A pairing code expires
after 15 minutes and works once, so generate a fresh one for each device. If
you use a custom state directory, pass the same `-state-dir` to `mewla pair`.

## Import on the phone

Open **Settings > Pair a server** in the app, then scan the QR code, import a
screenshot or photo of it, or paste the printed link. Opening a `mewla://` link
on the phone works too.

After pairing, the app reconnects with the stored server and device key. You
only need a new pairing code for a new device or after you clear the daemon's
state.

## Add another device

Once one device is paired, a new browser or phone can request access from any
HTTPS address of your daemon. The new device shows a three-digit number. A
paired device shows the request with several numbers; tap the matching one to
approve. A wrong choice shows an error, and denied or expired requests can be
retried. Requests expire after five minutes.

You can do the same on the computer:

```sh
mewla devices pending
mewla devices approve -id REQUEST_ID -number 042
mewla devices deny -id REQUEST_ID -number 042
```

## Pair a browser

The daemon serves the same app as a web page. To open it on the computer that
runs Mewla:

```sh
mewla web            # opens a paired browser tab
mewla web -no-open   # prints the one-time link instead
```

The link works once and expires after 15 minutes; do not share it. Later visits
to `http://127.0.0.1:9876/` reuse the pairing.

By default the page is served only to the same computer; `mewla --lan` does not
expose it over plain HTTP. To use it from another device, put Mewla behind an
HTTPS address that keeps the original `Host` header, allow that origin, and pair:

```sh
mewla -web-origin https://mewla.example.com
# in another terminal on the same computer:
mewla web -origin https://mewla.example.com -no-open
```

The browser gets the same Brain, Sessions, terminal and attachment uploads as
the phone. It has no QR scanning, push notifications or in-app browser, and it
keeps its device key in the browser's local storage, which is weaker than the
phone's keychain. Pair only browser profiles you control.

## Manage addresses

The daemon keeps the addresses it can be reached at, which `mewla pair` uses when
you give it no origin. It learns an HTTPS address after a signed request
arrives there. You can edit the list without a restart:

```sh
mewla address list
mewla address add https://mewla.example.com
mewla address remove https://mewla.example.com
```

## Revoke a device

```sh
mewla devices list
mewla devices revoke -id <device-id>
```

Revoking removes the device's key, closes its live connections and rejects its
later requests. Browsers appear as `Mewla Web (...)`. Removing a server in the
app's Settings only forgets it on that phone; it does not revoke the key.

## Mewla Link

Mewla Link is an optional relay that keeps the daemon on loopback and connects
outward. It works only after an operator configures relay infrastructure and a
`link.json` file in the state directory. No hosted Mewla Link service is
operated, so most people should use one of the routes above.

If something does not connect, see
[Troubleshooting](troubleshooting.md#the-phone-cannot-connect).
