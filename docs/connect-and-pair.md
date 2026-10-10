# Connect and pair

A phone or browser needs two things to use Mewla on your computer:

- **An address**: a URL where it can reach this computer.
- **Pairing**: a one-time code that gives that one device access.

`mewla address list` shows every address. `mewla pair` prints the code.
`mewla devices` lists who has access.

The daemon has its own Ed25519 key. During pairing the device enrolls its own
key once with a short-lived code; after that it signs every request. There is
no shared password.

## Choose an address

| Address | Use it when | Start the daemon with |
| --- | --- | --- |
| This computer (`http://127.0.0.1:9876`) | A browser on the same computer | `mewla` |
| [Wi-Fi/LAN](#wi-fi-or-lan) | Phone and computer share a trusted network | `mewla --lan` |
| [Tailscale](#tailscale) | Private access from anywhere | `mewla --lan`, or `mewla -addr "$(tailscale ip -4):9876"` |
| [HTTPS tunnel or reverse proxy](#https-tunnel-or-reverse-proxy) | A stable HTTPS address without a VPN app | `mewla`, then `mewla address add https://…` |
| [Mewla Link](#mewla-link) | You run your own relay | `mewla` with a `link.json` |

Mewla finds the first three itself each time it starts: this computer when it
listens on loopback or on all networks, and the Wi-Fi/LAN and Tailscale
addresses it listens on. It prints them at startup:

```text
  Mewla v0.2.7 · listening on all networks, port 9876
  Wi-Fi/LAN   http://192.168.x.x:9876
  Tailscale   http://100.x.y.z:9876
  Pair        mewla pair
```

### Wi-Fi or LAN

```sh
mewla --lan
```

The phone must reach the computer on port `9876`; a host firewall or Wi-Fi
client isolation can block it. LAN traffic is plain HTTP, so use this only on a
network you trust. `--lan` listens on every IPv4 interface.

### Tailscale

Use [Tailscale](https://tailscale.com/docs/install) when the phone should
connect privately from cellular data or another network.

1. Install Tailscale on the computer and the phone and sign both into the same
   tailnet. Make sure your tailnet's access rules let the phone reach the
   computer.
2. Start Mewla on the Tailscale address only, which keeps port `9876` closed on
   your local network:

   ```sh
   mewla -addr "$(tailscale ip -4):9876"
   ```

   `mewla --lan` also works; it listens on Wi-Fi/LAN as well.
3. Check that the phone can open `/health` at the Tailscale address Mewla
   printed, then run `mewla pair`.

Traffic stays inside Tailscale's encrypted network. For HTTPS, put
`tailscale serve` in front of `127.0.0.1:9876` and add its `https://…ts.net`
address as in the next section.

### HTTPS tunnel or reverse proxy

Use this when you want a stable HTTPS address and do not want a VPN app on the
phone. With Cloudflare, the domain must be on your Cloudflare account.

1. Keep Mewla on loopback:

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
   real hostname), then add the address and pair:

   ```sh
   mewla address add https://mewla.example.com
   mewla pair
   ```

Any other HTTPS reverse proxy works the same way if it forwards the full origin,
keeps the original `Host` header and passes WebSocket upgrades to
`http://127.0.0.1:9876`. Mewla also serves the web app at every HTTPS address
in the list.

The published hostname is reachable from the internet unless you add an access
layer. `/health` answers without authentication; everything else needs a
pairing code or a paired device's signature. A Cloudflare Access login page is
not supported by the app. Keep pairing links private.

## Pair

With the daemon running:

```sh
mewla pair
```

It prints one code, usable once by whichever device uses it first, in every
form you might need:

- **Phone:** a QR code and a `mewla://` link. The QR uses Mewla Link when it is
  configured; otherwise the best address in the list: HTTPS, then Tailscale,
  then Wi-Fi/LAN. A phone can't reach `127.0.0.1`, so that address is never
  in the QR. To use one particular address, pass it:
  `mewla pair https://mewla.example.com`.
- **Browser on this computer:** an `http://127.0.0.1:9876/#pair=…` link.
- **Browser anywhere:** an `https://…/#pair=…` link for each HTTPS address.

If no phone can reach the computer yet, `mewla pair` says so and still prints
the browser link. A code expires after 15 minutes. If you use a custom state
directory, pass the same `-state-dir` to `mewla pair`.

The first time the daemon starts with no paired device, it prints the same
block by itself.

### On the phone

Open **Settings > Pair a computer** in the app, then scan the QR code, import a
screenshot or photo of it, or paste the printed link. Opening a `mewla://` link
on the phone works too.

After pairing, the app reconnects with the stored address and device key. To
change the address later, edit the computer in **Settings**.

The app can keep several paired computers, but it connects to only one at a
time: the current one. Switching in **Settings** disconnects from the previous
computer and clears its Sessions, Brain, Work and Calendar from the app before
the new one loads.

### In a browser

Open the browser link `mewla pair` printed. Later visits to the same address
reuse the pairing.

The web app is served only to this computer (`127.0.0.1`) and at HTTPS
addresses; `mewla --lan` does not expose it over plain HTTP. The browser gets
the same Brain, Sessions, terminal and attachment uploads as the phone. It has
no QR scanning, push notifications or in-app browser, and it keeps its device
key in the browser's local storage, which is weaker than the phone's keychain.
Pair only browser profiles you control.

## Add another browser

Once one device is paired, a new browser can ask for access at any HTTPS
address of your computer. It shows a three-digit number. A paired device shows
the request with several numbers; tap the matching one to approve. A wrong
choice shows an error, and denied or expired requests can be retried. Requests
expire after five minutes.

You can answer on the computer too:

```sh
mewla devices pending
mewla devices approve -id REQUEST_ID -number 042
mewla devices deny -id REQUEST_ID -number 042
```

## Manage addresses

```sh
mewla address list
mewla address add https://mewla.example.com
mewla address remove https://mewla.example.com
```

`list` shows each address, its kind (This computer, Wi-Fi/LAN, Tailscale,
HTTPS or Mewla Link) and how Mewla knows it:

- **detected**: found when the daemon started. Detected addresses are refreshed
  at each start, so an old network's address disappears.
- **added**: added with `mewla address add`.
- **seen**: an HTTPS address a paired device used.

Changes take effect without a restart.

## Revoke a device

```sh
mewla devices list
mewla devices revoke -id <device-id>
```

Revoking removes the device's key, closes its live connections and rejects its
later requests. Browsers appear as `Mewla Web (...)`. Removing a computer in the
app's Settings only forgets it on that phone; it does not revoke the key.

## Mewla Link

Mewla Link is an optional relay that keeps the daemon on loopback and connects
outward. It works only after an operator configures relay infrastructure and a
`link.json` file in the state directory. No hosted Mewla Link service is
operated, so most people should use one of the addresses above.

If something does not connect, see
[Troubleshooting](troubleshooting.md#the-phone-cannot-connect).
