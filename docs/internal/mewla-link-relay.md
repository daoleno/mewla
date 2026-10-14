# Mewla Link Relay operations

This runbook describes a single-region MVP. Mewla operates one hosted relay
and account service for remote access (`connect.mewla.ai`, computers under
`*.m.mewla.ai`), deployed from this repository with Kamal; see
[Hosted remote access](#hosted-remote-access). There is no global SLO or
backbone. Self-hosted Link examples stay inert until an operator supplies
configuration, credentials, certificates and DNS.

The relay runs in two modes, and one relay may accept both:

- **Hosted remote access** (`mewla login`): the account service
  (`cmd/mewla-account`) signs a short-lived route grant per machine; the relay
  verifies it with `-grant-public-key-file`. See
  [Hosted remote access](#hosted-remote-access).
- **Self-hosted**: daemons share one connector token
  (`MEWLA_LINK_CONNECTOR_TOKEN`) and a `link.json`, described below.

## Ports and prerequisites

The relay has three listeners:

| Listener                   |          Default | Exposure                                    |
| -------------------------- | ---------------: | ------------------------------------------- |
| opaque mobile L4           |           `:443` | public TCP; wildcard client DNS points here |
| connector control/data TLS |          `:8443` | reachable from daemon hosts                 |
| operator health/metrics    | `127.0.0.1:8080` | private monitoring only                     |

The control listener needs a normal CA-valid TLS certificate/key. Each client
candidate needs a wildcard DNS record such as `*.us1.link.example` reaching the
raw L4 listener. The wildcard name is routing only: the certificate inside that
connection is daemon-generated and Pairing V2 pinned, not a relay wildcard
certificate.

Generate a high-entropy operator connector token using your normal secret
manager. Do not put it in source control, shell history, images, logs, pairing
links, or mobile config.

## Build and run

From the repository root:

```bash
docker build -f daemon/Dockerfile.relay -t mewla-relay:local .
```

Direct binary:

```bash
cd daemon
go build -o ../bin/mewla-relay ./cmd/mewla-relay

MEWLA_LINK_CONNECTOR_TOKEN='from-your-secret-manager' \
../bin/mewla-relay \
  -client-addr=:443 \
  -control-addr=:8443 \
  -operator-addr=127.0.0.1:8080 \
  -tls-cert=/secure/path/control.crt \
  -tls-key=/secure/path/control.key
```

The image runs as non-root, so map public TCP `443` to an unprivileged
container port (the hosted deployment uses `9443`). The direct binary defaults
to `:443`, so a bare-host operator should either set `-client-addr` behind a
load balancer or grant only the platform's narrow low-port binding capability.
Keep the control certificate and key out of the repository; mount them from a
secret manager.

## Daemon configuration

Create `<state-dir>/link.json` (default `~/.mewla/link.json`) with mode `0600`:

```json
{
  "version": 1,
  "connector_token_env": "MEWLA_LINK_CONNECTOR_TOKEN",
  "max_streams": 32,
  "relays": [
    {
      "name": "us-east",
      "control_address": "control.us-east.link.example:8443",
      "control_server_name": "control.us-east.link.example",
      "client_domain": "us-east.link.example",
      "client_port": 443
    },
    {
      "name": "eu-west",
      "control_address": "control.eu-west.link.example:8443",
      "control_server_name": "control.eu-west.link.example",
      "client_domain": "eu-west.link.example",
      "client_port": 443
    }
  ]
}
```

For a private/local CA, set `control_ca_file`; relative paths resolve against
the config directory. `connector_token` is supported for constrained local
testing, but `connector_token_env` is preferred. The two are mutually
exclusive.

Start the daemon with that environment:

```bash
MEWLA_LINK_CONNECTOR_TOKEN='from-your-secret-manager' mewla
```

An explicit config elsewhere uses `mewla -link-config /path/link.json`. The
daemon continues listening on loopback and opens only outbound Link sockets.
After it connects:

```bash
MEWLA_LINK_CONNECTOR_TOKEN='from-your-secret-manager' mewla pair
```

If Link is not configured, `mewla pair` uses the best address in
`mewla address list` (or `mewla pair <address>`); it remains Pairing V1.

## Health, readiness, and metrics

```bash
mewla-relay -check=http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/metrics
```

Only these aggregate series are exported:

- active routes
- active streams
- accepted streams total
- rejected connections total
- forwarded bytes total

Never add route/SNI/daemon/device/path/token/filename labels. Alert on
readiness failure, restart loop, sustained rejection growth, connection
capacity, file-egress growth, and connector reconnect SLO.

## Limits and cost model

Important flags:

- `-max-clients`
- `-max-clients-per-route`
- `-max-clients-per-route-ip` (default half of the route cap: one source IP
  never holds every slot of a computer, even with streams the daemon already
  closed but the client keeps half-open)
- `-max-client-handshakes`
- `-max-connector-handshakes`
- `-handshake-timeout`
- `-attach-timeout`
- `-idle-timeout` (fires only when neither stream direction makes progress;
  one-way transfers remain active and half-closed requests may drain responses)

Cost is principally public egress bytes, concurrent client + connector sockets,
kernel memory, TLS CPU, and regional load-balancer/DDoS service. Large file
traffic can dominate cost: the daemon permits 2 GiB per upload, 8 GiB aggregate
retained storage, and seven-day retention. Relay copies are streaming and do
not store the body.

## Local black-box E2E

The automated test starts real TCP listeners, relay, connector, daemon-side
inner TLS, and full-origin handler:

```bash
cd daemon
go test -run TestOpaqueRelayConnectorHealthAndAdmissionReplay -v ./link

# Also execute the real TS importConnection owner through an on-demand
# loopback pinned-transport adapter, the same Relay/Connector, and daemon /pair.
MEWLA_LINK_MOBILE_RELAY_E2E=1 \
  go test -run TestOpaqueRelayConnectorHealthAndAdmissionReplay -v ./link
```

It proves:

- empty TLS preflight and a wrong HTTP path do not consume admission
- the actual `POST /pair` atomically consumes admission and replay fails
- wrong SPKI pin fails
- `/pair`, `/auth-check`, `/health`, `/upload`,
  `/session-file-capability`, `/session-file` Range, and `/ws` traverse opaque
  relay streams
- a blocked upload does not prevent a Terminal-like WebSocket round trip
- relay metrics contain aggregate metadata, not the payload sentinel

Security/unit coverage also includes expired/replayed connector auth, wrong
daemon binding, stream-ticket replay, unknown/expired route admissions,
slowloris deadline, and over-concurrency rejection:

```bash
cd daemon
go test ./relay ./link ./auth ./server
go test -race ./relay ./link
```

No multi-GiB fixtures are created.

## Single-region rollout and rollback

Rollout order:

1. Run one relay process in one region.
2. Validate health/readiness, metadata-only metrics, admission, pair, probe,
   WSS/Terminal, upload, and Range.
3. Distribute `link.json` only to opt-in daemon hosts.
4. Keep explicit V1/LAN/Tailscale/Cloudflare paths unchanged.
5. Add a second region as a Pairing V2 candidate only after connector/mobile
   failover testing.

Upgrade one active single-region process with drain time. SIGTERM stops new
listeners, cancels active forwarding, and causes connectors to re-register.
The reconnect target is p95 ≤10 seconds and p99 ≤35 seconds, but that is an
acceptance target, not a deployed SLO claim.

The atomic pairing-admission commit is control protocol/ALPN version 2. Relay
and Connector deliberately reject a mixed v1/v2 control connection instead of
silently losing replay protection. Upgrade the single-region Relay and opted-in
daemons in the same maintenance window; self-managed transports do not depend
on that window.

Rollback:

1. Remove/rename `link.json` and restart the daemon in the operator's normal
   maintenance window, or launch without `-link-config`.
2. Existing self-managed server records and Pairing V1 links continue working.
3. Stop the relay after connectors drain.
4. Do not delete daemon identity, trusted-device, or Link identity state merely
   to roll back transport. Preserving the Link identity avoids needless pin
   rotation if Link is re-enabled.

## Multi-region boundary

MVP route presence is process memory. One active process per region is the
honest deployment. Candidate failover works across separately named regions;
it does not make multiple replicas behind one uncoordinated load balancer safe.
A later route-owner directory or deterministic shard is required before
horizontal scale.

GeoDNS/Anycast may steer a region hostname to its regional process, with
`/healthz` and `/readyz` as probes. Keep each candidate hostname explicit in
Pairing V2. Do not build a custom backbone, SD-WAN, or P2P/NAT traversal layer
for this MVP.

## Hosted remote access

`config/deploy.yml` deploys the hosted service with Kamal on one host
(OVH `167.114.116.27`, shared with Onlora):

| Name and port | Served by |
| --- | --- |
| `connect.mewla.ai:443` | The relay passes the TLS stream, by SNI, to the account service (`-sni-route`), with a PROXY protocol v1 header carrying the client's address (`-sni-route-proxy-protocol`). The account service holds its own Let's Encrypt certificate (`-tls-addr`, TLS-ALPN-01 through the relay) and reads the header (`-proxy-protocol`). |
| `connect.mewla.ai:8443` | The relay's control plane for daemons. Self-signed certificate that the account service hands to each computer, which pins it. |
| `*.m.mewla.ai:443` | The relay, by SNI label, to that computer. The computer holds the certificate; the relay sees only encrypted bytes. |

| Kamal piece | Role |
| --- | --- |
| `account` role (`mewla-link-account-<version>`) | Device-code login (GitHub, shown as `https://connect.mewla.ai/device`), machine registry (SQLite), route grants, DNS-01 TXT records for each computer's certificate (answered only once the zone's authoritative nameservers serve the record, at most 20 s, because the daemon asks the CA to validate right away), quotas, 10 sign-in attempts per client address an hour. Not published; network alias `mewla-account` on the `kamal` network. |
| `relay` accessory (`mewla-link-relay`) | Publishes host `:443` and `:8443` (IPv4) and its operator listener on `127.0.0.1:9180`. |

Both run from one image (`daemon/Dockerfile.link`). Nothing uses kamal-proxy.

Trust boundaries:

- A grant binds account, machine, route ID, hostname label and the daemon's
  Ed25519 key, and lives `-grant-ttl` (1h). The relay rejects a register,
  admission or renewal whose grant does not match the signing daemon key and
  route, and closes a route when its grant expires. Disabling an account or
  `mewla logout` stops new grants, so the computer drops off within one TTL.
- A computer's certificate key never leaves it. The account service only
  writes `_acme-challenge.<that computer's hostname>` TXT records, and only
  for requests signed by that computer's key.
- The operator controls DNS, so it could obtain a certificate for any
  hostname and intercept browser traffic. Phones pin the computer's key and
  are not exposed to that. Say so in user-facing docs; monitor CT logs.
- Hostnames appear in Certificate Transparency logs. They are addresses, not
  secrets; daemons limit unauthenticated streams per route and the relay
  limits handshakes and streams per source IP.
- The relay reports each client's IP in `open_stream` (`client_addr`). The
  daemon uses it only to share limits between clients: the enrollment rate
  limit (5 a minute per IPv4 address or IPv6 /64) and which unproven stream
  to close first (the oldest of the client holding the most). It never grants
  trust; loopback checks keep using the connection's own address. A relay
  that lies about it can only spend its own clients' allowance. Two or more
  addresses together can still fill the daemon's 32 pending enrollments for
  their 5-minute lifetime; the person approving sees the flood in the
  terminal. So a label never comes from
  the computer: without a name the service picks `adjective-noun-<4 hex>`;
  `mewla login -name <word>` (`PUT /v1/machine/name`) sets `<word>-<6 hex>`
  and keeps the machine and route, so phones are unaffected. A label is a
  browser origin, so labels released by a rename or logout are kept in
  `retired_labels` and never assigned again: the next holder could otherwise
  read an earlier browser's stored device key.

Relay flags for hosted mode: `-grant-public-key-file` (hex Ed25519 keys, one
per line), `-sni-route host=addr` (repeatable), `-sni-route-proxy-protocol`
(PROXY v1 header on static routes; the target must accept only the relay),
`-max-client-handshakes-per-ip`
(64), `-max-clients-per-ip` (64), `-max-connector-handshakes-per-ip` (32),
`-max-route-bindings` (65536, token mode). The relay logs route events with
an 8-hex hash prefix, never route IDs, labels, SNI or IP addresses.

### Certificate issuance budget

Let's Encrypt allows 50 new certificates per registered domain every 7 days,
counted across all accounts (refilling one every 202 minutes). Until the
client domain is on the Public Suffix List, every computer's hostname shares
that one budget. Renewals with the same hostname are exempt (they only count
against 5 per exact name per 7 days), so the budget is spent by new labels:
a first sign-in, a sign-in after `mewla logout`, and `mewla login -name`
(limited to 3 renames per account per day for this reason). That is about
50 new computers a week at launch.

In order of preference:

1. Submit the client domain to the Public Suffix List (private section) on
   day one. Once it lands, each label is its own registered domain with its
   own 50/week. Inclusion takes weeks to months and reaches clients slowly.
2. File the Let's Encrypt rate limit adjustment request
   (https://isrg.formstack.com/forms/rate_limit_adjustment_request) for the
   client domain. It also takes weeks.
3. While both are pending, count `account: machine created` and
   `account: machine renamed` log lines per 7 days (`kamal logs`); past ~40,
   pause new sign-ups before new computers start getting no browser
   certificate. Renewals keep working either way. There is no switch for
   that yet, and the GitHub OAuth app is shared with the GitHub plugin, so
   disabling it would break that too; add a flag to `mewla-account` first.

Not an option: a wildcard certificate. The operator would hold its key, which
ends "the certificate key never leaves the computer" and lets the operator
read browser traffic without leaving a CT trace per computer.

A second CA is not a flag flip. `-acme-directory` switches the CA, but
ZeroSSL and Google Trust Services, the free alternatives, require External
Account Binding (EAB): a key ID and HMAC from the operator's CA account
that binds each new ACME account to it. Every computer registers its own
ACME account, so each one would need a binding:

- Shared ZeroSSL EAB (reusable since March 2022): hand the same KID and HMAC
  to every computer in `MachineInfo.acme`. Small, but anyone who signs in
  gets the operator's credential and can open ACME accounts under the
  operator's ZeroSSL user until it is rotated. Not built; acceptable only as
  a short emergency measure, with the credential rotated afterwards.
- Service-computed binding (the right design): the computer sends its ACME
  account public key, the account service returns the EAB JWS (HMAC over
  that key and the newAccount URL), and the HMAC never leaves the service.
  `golang.org/x/crypto/acme` only computes EAB from the raw HMAC, so the
  daemon must send its own newAccount request. A few hundred lines with
  tests; build it if the PSL entry is still pending when sign-ups approach
  the budget. Configure it with `-acme-directory` plus an EAB KID flag and
  the HMAC from the environment.
- Google Trust Services EAB keys are single-use: each registers one ACME
  account. The service would mint one per computer through the Public CA
  API (`gcloud publicca external-account-keys create`); a leaked key is
  worth at most one account. More moving parts than ZeroSSL.

Before switching CA, publish CAA for the client domain that allows it:
`0 issue "letsencrypt.org"` plus `0 issue "sectigo.com"` (ZeroSSL) or
`0 issue "pki.goog"` (Google Trust Services), and `0 issuewild ";"`.

### Operations (Kamal)

Run everything from the repository root with Kamal 2.12
(`gem install kamal -v 2.12.0`, or `BUNDLE_GEMFILE=~/workspace/onlora/Gemfile
bundle exec kamal`). Local inputs, never committed:

| Path | Contents |
| --- | --- |
| `.kamal/secrets` | Copy of `.kamal/secrets.example`; reads the Cloudflare token from `~/.config/mewla-remote/cloudflare-dns-token` (0600) when Kamal loads secrets. The token is limited to DNS edit on the `mewla.ai` zone. |
| `.kamal/files/relay.crt`, `relay.key` | The relay's self-signed control certificate (P-256, `CN=connect.mewla.ai`). |
| `.kamal/files/grant.pub` | Hex grant public keys the relay accepts, one per line. |
| `~/.config/mewla-remote/backup/` (0700) | Copies of the three files above and volume backups (0600). |

The host limits SSH to six new connections per 30 seconds per address and
then drops it for minutes. Each Kamal command opens at least one; leave about
30 seconds between commands.

Images are tagged with the full commit SHA. Pass it explicitly, because a
dirty working tree makes Kamal append `_uncommitted_…`:

```sh
V=$(git rev-parse HEAD)
```

**First deploy** (already done; repeat only on a new host):

1. DNS, unproxied: `A connect.mewla.ai` and `A *.m.mewla.ai` to the host.
2. Control certificate:
   ```sh
   (umask 077; mkdir -p .kamal/files && cd .kamal/files &&
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 825 \
      -keyout relay.key -out relay.crt -subj /CN=connect.mewla.ai \
      -addext subjectAltName=DNS:connect.mewla.ai && chmod 644 relay.crt)
   ```
3. `kamal build deliver --version $V`
4. Grant key, created inside the account volume:
   ```sh
   ssh ubuntu@167.114.116.27 "docker run --rm -v mewla-link-account:/var/lib/mewla-account \
     localhost:5555/daoleno/mewla-link:$V /usr/local/bin/mewla-account \
     -grant-key-file=/var/lib/mewla-account/grant-key.pem -print-public-key" > .kamal/files/grant.pub
   ```
5. `MEWLA_LINK_VERSION=$V kamal accessory boot relay`
6. `kamal deploy --skip-push --version $V`. The account service obtains its
   certificate on the first HTTPS handshake.

The host's `DOCKER-USER` guard (`/etc/ufw/after.rules`, "SRT DOCKER GUARD")
matches container ports after DNAT and allows 22, 80, 443 and 8443, which is
why the relay listens on 443 and 8443 inside its container. Onlora's
kamal-proxy publishes only `:80` (`proxy.run` in Onlora's
`config/deploy*.yml`). A `kamal proxy reboot` from an Onlora checkout without
that setting would try to bind `:443`, fail, and take Onlora down; a normal
`kamal deploy` restarts the existing proxy container and is safe.

**Deploy the account service:** commit, then `kamal deploy --version $V`.
The new container starts before the old one stops; both share the SQLite
volume for a few seconds.

**Upgrade the relay** (drops every stream; daemons reconnect within seconds):

```sh
export MEWLA_LINK_VERSION=$V   # an image already on the host
kamal accessory stop relay
kamal accessory remove_container relay
kamal accessory boot relay
```

`kamal accessory reboot relay` does not work: it pulls the image from the
local registry, which is only tunnelled to the host during builds.

**Roll back:** `kamal app containers` and
`ssh ubuntu@167.114.116.27 docker images localhost:5555/daoleno/mewla-link`
list the retained versions (the last five). The account service:
`kamal rollback <version>`. The relay: the upgrade sequence with the old
version.

**Logs and metrics:**

```sh
kamal logs                     # account service
kamal relay-logs               # relay
ssh ubuntu@167.114.116.27 curl -fsS http://127.0.0.1:9180/readyz
ssh ubuntu@167.114.116.27 curl -fsS http://127.0.0.1:9180/metrics
kamal app exec --reuse "/usr/local/bin/mewla-account -check=http://127.0.0.1:8080/healthz"
```

The relay's operator listener is published only on the host's loopback; the
account service's `/healthz` is only on its internal `:8080` and returns 404
over HTTPS.

**Rotate the grant key** (no sign-out):

1. Create the new key next to the old one and add its public key:
   ```sh
   ssh ubuntu@167.114.116.27 "docker run --rm -v mewla-link-account:/var/lib/mewla-account \
     localhost:5555/daoleno/mewla-link:$V /usr/local/bin/mewla-account \
     -grant-key-file=/var/lib/mewla-account/grant-key-2.pem -print-public-key" >> .kamal/files/grant.pub
   ```
2. Upgrade the relay (same version) so it accepts both keys.
3. Point `-grant-key-file` in `config/deploy.yml` at `grant-key-2.pem`,
   commit and deploy the account service.
4. After one grant lifetime (1 hour), delete the old line from
   `.kamal/files/grant.pub`, upgrade the relay again, remove the old key file
   from the volume, and take a backup.

**Back up** the account volume (SQLite database, grant key, certificate
cache). The database is in WAL mode, so stop the service for a consistent
copy; computers keep working on their current grant meanwhile:

```sh
kamal app stop
ssh ubuntu@167.114.116.27 "docker run --rm -v mewla-link-account:/d:ro alpine:3.20 tar -C /d -cf - ." \
  > ~/.config/mewla-remote/backup/account-$(date +%Y%m%d-%H%M%S).tar
kamal app start
chmod 600 ~/.config/mewla-remote/backup/*.tar
```

**Restore** onto an empty or damaged volume:

```sh
kamal app stop
ssh ubuntu@167.114.116.27 "docker run --rm -i -v mewla-link-account:/d alpine:3.20 \
  sh -c 'find /d -mindepth 1 -delete && tar -C /d -xpf -'" < ~/.config/mewla-remote/backup/account-<stamp>.tar
kamal app start
```

If the grant key is lost, generate a new one (first deploy, step 4), replace
`grant.pub`, upgrade the relay and deploy; every computer must then sign in
again.

### Local end-to-end environment

Pebble and `pebble-challtestsrv` stand in for Let's Encrypt and DNS:

```sh
pebble-challtestsrv -dnsserver 127.0.0.1:8053 -management 127.0.0.1:8055 \
  -http01 "" -https01 "" -tlsalpn01 "" -doh ""
PEBBLE_VA_NOSLEEP=1 pebble -config pebble.json -dnsserver 127.0.0.1:8053
mewla-account -addr 127.0.0.1:18090 -db account.db -grant-key-file grant.pem \
  -public-url http://127.0.0.1:18090 -client-domain mewla.test -client-port 19443 \
  -relay name=local,control=127.0.0.1:18443,server-name=relay.mewla.test,ca-file=relay.crt \
  -acme-directory https://127.0.0.1:14000/dir -acme-ca-file pebble-ca.pem \
  -dev-fake-login -dns-provider challtestsrv -challtestsrv-url http://127.0.0.1:8055
mewla-relay -client-addr 127.0.0.1:19443 -control-addr 127.0.0.1:18443 \
  -operator-addr 127.0.0.1:18081 -tls-cert relay.crt -tls-key relay.key \
  -grant-public-key-file grant.pub
MEWLA_REMOTE_SERVICE_URL=http://127.0.0.1:18090 mewla
```

`-dev-fake-login` lets anyone sign in as anyone at `/device`; never use it on
a reachable host. Browsers need the Pebble root
(`https://127.0.0.1:15000/roots/0`) and a resolver rule for `*.mewla.test`.
