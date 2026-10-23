# Connect and pair

A phone or browser needs two things to use Mewla on your computer:

- **An address**: a URL where it can reach this computer.
- **Pairing**: a one-time code that gives that one device access.

`mewla address list` shows every address. `mewla pair` prints the code.
`mewla web` opens the web UI in this computer's browser and pairs it.
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
| [Remote access](#remote-access) | Phone and browsers on any network, nothing to set up | `mewla login` |
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

It prints one code, usable once by whichever device uses it first, on one
screen: whether remote access is on and how many devices are paired, a QR code
for the phone with the time it expires, and a short link for a browser on this
computer (`http://127.0.0.1:9876/p/…`, which opens the pairing page and works
only from this computer).

The QR uses Mewla Link when it is configured; otherwise the best address in
the list: HTTPS, then Tailscale, then Wi-Fi/LAN. A phone can't reach
`127.0.0.1`, so a loopback address (`http` or `https`) is never in the QR.
When remote access is on but the relay does not issue a pairing admission,
`mewla pair` says why (for example that the relay closed the connection
without answering, and after how long) before falling back to those
addresses. To use one particular address,
pass it: `mewla pair https://mewla.example.com`. When the terminal is too narrow
for the QR, it says how wide it needs to be instead of drawing a broken code.

With remote access or Mewla Link, the QR holds a compact signed code (about 67
columns by 33 rows). It carries the signed route, transport pin and one-time
admission. The app 0.2.17 or later reads it; a code from an older computer's
longer format no longer pairs, so update both sides and run `mewla pair` again.

`mewla pair --details` prints the same code in every other form:

- **Phone:** the `mewla://` link to paste into the app.
- **Browser on this computer:** the full `http://127.0.0.1:9876/#pair=…` link.
- **Browser anywhere:** an `https://…/#pair=…` link for each HTTPS address.
- How to revoke a device.

If no phone can reach the computer yet, `mewla pair` says so and still prints
the browser link. A code expires after 15 minutes. If you use a custom state
directory, pass the same `-state-dir` to `mewla pair`.

The first time the daemon starts with no paired device, it prints the same
screen by itself. Running `mewla` while Mewla is already running does the
same when nothing is paired yet; otherwise it shows the status lines and
points to `mewla pair`.

Colors and the arrow-key menu appear only in an interactive terminal. With
`NO_COLOR` set, `TERM=dumb`, in CI or when the output goes to a file or a log,
the screens are plain text and menus take a number.

### On the phone

On the app's welcome screen tap **Mewla already running? Scan its code**, or
open **Settings > Pair a computer** in the app, then scan the QR code, import a
screenshot or photo of it, or paste the printed link. Opening a `mewla://` link
on the phone works too.

After pairing, the app reconnects with the stored address and device key. The
computer shows up under the name it calls itself, such as your Mac's name. To
rename it or change the address later, tap it in **Settings**.

The app can keep several paired computers, but it connects to only one at a
time: the current one. The computer at the bottom of the menu is the one in
use; tap it to switch to another paired computer, or to open **Settings >
Computers** (with a single computer it goes straight there). Switching there
or in **Settings** disconnects from the previous computer and clears its Sessions, Brain, Work and
Calendar from the app before the new one loads.

### In a browser

On the computer running Mewla:

```sh
mewla web
```

It opens `http://127.0.0.1:9876` in the default browser through a one-time
pairing link, so the browser is paired the first time and goes straight in
after that. A browser that is already paired reuses its device; it is not added
again. It also prints the addresses:

```text
■ Mewla web UI
  This computer   http://127.0.0.1:9876 · opened in your browser
  Remote          https://quiet-otter-3f9a.m.mewla.ai
                  Open it in a browser on another computer, then match
                  its number here: mewla devices pending
  Wi-Fi/LAN       http://192.168.x.x:9876 · phone app only
```

The remote access line appears after `mewla login`; open that address on any
other computer and [approve its number](#add-another-browser). Other HTTPS
addresses are listed too. Wi-Fi/LAN and Tailscale addresses are for the phone
app: the web app is not served over plain HTTP.

Over SSH, without a display, or with `--print`, nothing is opened: it prints
the addresses and the one-time link instead. Over SSH, forward the port first
(`ssh -L 9876:127.0.0.1:9876 <computer>`) and open the link on your own
computer. If Mewla is not running, it says so; start it with `mewla`.

The browser link `mewla pair` prints works the same way. Later visits to the
same address reuse the pairing.

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
- **seen**: an HTTPS address an earlier version learned from a paired device's
  request. Mewla no longer learns addresses from the `Host` header: an address
  is detected, added by you or set by `mewla login`.

Changes take effect without a restart.

## Revoke a device

```sh
mewla devices list
mewla devices revoke -id <device-id>
```

Revoking removes the device's key, closes its live connections and rejects its
later requests. Browsers appear as `Mewla Web (...)`. Removing a computer in the
app's Settings first has your other computers forget it, then asks that
computer to revoke the phone, for up to three
seconds; if the computer is offline the phone forgets it anyway and the key
stays trusted until you revoke it here.

Pairing is idempotent for one phone: if a pairing answer is lost on the way
back (a dropped connection, or the same QR scanned twice), the phone checks
with its own key whether the computer already trusts it and finishes
connecting instead of reporting a failure. The daemon also answers a repeat of
a used code from the same device key, until the code would have expired.

## Remote access

`mewla login` gives this computer an HTTPS address such as
`https://quiet-otter-3f9a.m.mewla.ai`. Your phone and any browser reach it
from any network; the computer only connects outward, so there is no port to
open, no VPN and no domain to buy. The first time you start `mewla` by hand it
offers the same choice.

```sh
mewla login     # shows https://connect.mewla.ai/device and a code; sign in with GitHub there
mewla pair      # the phone QR now uses remote access
mewla login -name studio   # rename the address, e.g. https://studio-c0ffee.m.mewla.ai
mewla logout    # turn it off
mewla logout -delete-account   # turn it off and delete your account
```

The address starts with two random words, never your computer's hostname,
because every certificate is published (see below). Rename it with
`mewla login -name <word>`; a short random suffix keeps it unique. Paired
phones keep working after a rename. A browser pairs again at the new address,
because its key belongs to the address it paired at. The old name stays in the
public certificate logs, and you can rename three times a day.

- **The account finds the computer; it does not unlock it.** Every phone or
  browser still pairs once. A browser shows a number and the computer's
  terminal prints the same number with the command that approves it, so a
  stolen account alone cannot use your computer.
- **The relay cannot read your traffic.** It forwards encrypted bytes by
  hostname. The computer holds the only private key for its certificate; it
  gets the certificate from Let's Encrypt through the service.
- **Phones pin this computer's key.** The phone app checks the computer's own
  key, not the certificate authority, so it stays private even from the
  service operator. Browsers rely on the public certificate system like any
  website; certificates for every hostname are public in Certificate
  Transparency logs, so the hostname is not a secret. Pick a name you are
  happy to publish.
- `mewla address list` and `mewla doctor` show the address, the relay
  connection and the certificate.

The service keeps your GitHub id and login, and each signed-in computer's
address and name. `mewla logout` forgets this computer.
`mewla logout -delete-account` asks you to type the account name, then deletes
the account and every computer signed in with it; the others drop off within
an hour. Run it from any computer still signed in with that account. See the
[privacy policy](https://mewla.ai/privacy/).

## Mewla Link

Mewla Link is the relay remote access runs on. You can also run your own
relay: configure relay infrastructure and a `link.json` file in the state
directory. See the operator guide in `docs/internal/mewla-link-relay.md`.

### Direct when it can

A phone paired through Mewla Link goes straight to the computer when it can
reach it on the same Wi-Fi/LAN or over Tailscale, and uses the relay otherwise.
The badge on the Screen page says which: **LAN**, **Tailscale** or **Relay**.

A computer that listens only on loopback, the default, also listens on its
Wi-Fi/LAN and Tailscale addresses (private networks: 10/8, 172.16/12,
192.168/16, 100.64/10) over TLS only, on the same port. Plain HTTP there is
refused, and only the routes a paired phone signs for are served there: not
pairing, join requests or the web UI. Mewla follows the addresses as networks
come and go. Each time the phone connects, the computer tells it the addresses
it is listening on, and the phone remembers them. `mewla --lan` is still how you
open plain HTTP, pairing and the web UI to your network.

The phone tries them while it also measures the relay. It uses a direct address
only after two proofs: the TLS connection to it is pinned to the computer's key
from pairing, and the computer has signed a fresh challenge from the phone with
its daemon key. Until then nothing from the phone, including the device
signature, is sent there. An address that has moved to another device, or that
answers wrongly, fails the proof and the relay is used. When the direct address
is lost the connection goes to the relay, which the phone keeps open for that,
and a phone on the relay moves to a direct address within about half a minute
of it becoming reachable. Moving between the two reconnects, so the Screen
viewer retries once on its own when its direct path ends.

If something does not connect, see
[Troubleshooting](troubleshooting.md#the-phone-cannot-connect).
