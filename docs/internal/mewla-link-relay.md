# Mewla Link Relay operations

This runbook describes an optional single-region MVP. All Link source and
deployment examples are inert until an operator explicitly supplies
configuration, credentials, certificates, DNS, and starts them. This
repository does not deploy or start a relay and does not claim that a
production endpoint, global SLO, or backbone exists.

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

Compose:

```bash
cd deploy/mewla-link
MEWLA_LINK_CONNECTOR_TOKEN='from-your-secret-manager' docker compose up --build
```

The Compose file maps public TCP `443` to unprivileged container port `9443`;
the distroless process remains non-root. The direct binary defaults to `:443`,
so a bare-host operator should either set `-client-addr` behind a load balancer
or grant only the platform's narrow low-port binding capability.

Place `tls.crt` and `tls.key` under `deploy/mewla-link/secrets/` only for local
operator testing; that directory must remain untracked. In production mount
from a secret manager instead.

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

`deploy/mewla-link/compose.yaml` runs the hosted service on one VPS:

| Service | Role |
| --- | --- |
| `relay` | Public `:443` (clients, SNI passthrough) and `:8443` (daemon control). `-sni-route` hands the service hostname to Caddy; every other hostname is a computer. |
| `account` | Device-code login (GitHub), machine registry (SQLite), route grants, DNS-01 TXT records for each computer's certificate, quotas. |
| `caddy` | HTTPS for the account service, behind the relay's SNI route; port 80 for redirects and HTTP-01. |

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
  limits handshakes and streams per source IP. So a label never comes from
  the computer: without a name the service picks `adjective-noun-<4 hex>`;
  `mewla login -name <word>` (`PUT /v1/machine/name`) sets `<word>-<6 hex>`
  and keeps the machine and route, so phones are unaffected.

Relay flags for hosted mode: `-grant-public-key-file` (hex Ed25519 keys, one
per line), `-sni-route host=addr` (repeatable), `-max-client-handshakes-per-ip`
(64), `-max-clients-per-ip` (64), `-max-connector-handshakes-per-ip` (32),
`-max-route-bindings` (65536, token mode). The relay logs route events with
an 8-hex hash prefix, never route IDs, labels, SNI or IP addresses.

### Launch checklist

1. Buy the domain. Use one zone for the service and relay
   (`account.<domain>`, `relay.<domain>`) and one wildcard for computers
   (`*.<client domain>`).
2. Apply for the client domain's inclusion in the Public Suffix List (private
   section), or request a Let's Encrypt rate limit adjustment. Without it the
   whole client domain gets 50 new certificates per 7 days. Both take weeks.
3. Create the VPS. Open TCP 80, 443 and 8443.
4. DNS: `A account.<domain>`, `A relay.<domain>` and `A *.<client domain>` to
   the VPS. Move the client zone to Cloudflare and create an API token with
   DNS edit on that zone only; note the zone ID.
5. Create a GitHub OAuth App (Settings → Developer settings → OAuth Apps),
   enable Device Flow, and note its client ID. No client secret is needed.
6. On the VPS, in `deploy/mewla-link/`:
   ```sh
   export MEWLA_SERVICE_HOST=account.<domain> MEWLA_RELAY_HOST=relay.<domain> \
     MEWLA_CLIENT_DOMAIN=<client domain> MEWLA_GITHUB_CLIENT_ID=… \
     MEWLA_CLOUDFLARE_TOKEN=… MEWLA_CLOUDFLARE_ZONE_ID=…
   mkdir -p secrets
   openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 825 \
     -keyout secrets/relay.key -out secrets/relay.crt -subj /CN=$MEWLA_RELAY_HOST \
     -addext subjectAltName=DNS:$MEWLA_RELAY_HOST
   docker compose run --rm account -grant-key-file=/var/lib/mewla-account/grant-key.pem \
     -print-public-key > secrets/grant.pub
   docker compose up -d --build
   ```
   The relay control certificate is self-signed; the account service hands it
   to each computer, which pins it.

   The relay's per-IP limits need real client addresses. Docker's userland
   proxy (IPv6, and some setups) shows every client as one gateway address;
   check `mewla_link_capacity_used{resource="clients_per_ip"}` after launch,
   and run the relay with `network_mode: host` if it does.
7. Build release daemons with
   `-ldflags "-X github.com/daoleno/mewla/daemon/remote.DefaultServiceURL=https://account.<domain>"`.
   Until then, `MEWLA_REMOTE_SERVICE_URL` or `mewla login -service` selects it.
8. Smoke test from a fresh machine: `mewla` → 1 → GitHub → `mewla pair`, then a
   browser on another network and a phone on cellular.
9. Back up the `account-data` volume (SQLite database and grant key). Losing
   the grant key signs every computer out until they log in again.

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
