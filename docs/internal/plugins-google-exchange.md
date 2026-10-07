# Google publisher exchange deployment artifact

This component is an unselected candidate prepared for review and fixture verification. Review the [maintained-component comparison](plugins-google-options.md) before choosing it. It is **not deployed**, and no publisher client or HTTPS origin has been registered. It is not part of a user's private daemon. Its only provider is Google; Slack uses its official public PKCE flow instead.

Build `daemon/cmd/mewla-google-auth` as a normal Go binary. Host it behind an existing product-owned TLS ingress. Required private environment configuration on that host:

| Name | Owner / value |
| --- | --- |
| `MEWLA_GOOGLE_AUTH_ORIGIN` | Confirmed product HTTPS origin, no path/query |
| `MEWLA_GOOGLE_CLIENT_ID` | Mewla-owned Google Web application client |
| `MEWLA_GOOGLE_CLIENT_SECRET` | Stored only in the publisher host's secret manager |
| `MEWLA_GOOGLE_RECEIPT_KEY` | At least 32 cryptographically random bytes, base64url without padding; durable publisher secret |
| `MEWLA_GOOGLE_AUTH_LISTEN` | Private listen address; defaults to `127.0.0.1:8098` |

Register exactly `ORIGIN/google/callback` in the Google client. Build official daemons with the **public** `connections.GoogleExchangeOrigin` set to that origin. End users receive no client secret and do no configuration. A Google Cloud project must enable Drive/Gmail/Calendar APIs and complete the applicable consent/verification requirements.

The exchange has five fixed routes: start, browser authorize, Google callback, status/cancel, and refresh. No caller may supply a provider, token endpoint, scope string, callback URL or daemon URL. The native app asks its current daemon to start. That daemon generates a NaCl box keypair and a random nonce/retrieval capability. The broker receives only the public key and capability over HTTPS. Its browser flow uses its own single-use state, S256 PKCE and a Secure/HttpOnly/SameSite browser cookie. The Google callback exchanges with the product-held secret, seals the token pair to the original daemon public key using maintained `x/crypto/nacl/box`, and offers only a fixed `zen://plugins` return. Browser HTML contains no code/token/retrieval capability.

Only the daemon holding the retrieval capability can poll/cancel, and only its private key can decrypt. Decrypted results include the original nonce. The daemon verifies vendor identity and scopes, then saves the token pair and refresh binding in its existing private credential vault. Pending accounts are absent from its public account list. It deletes the exchange's pending entry after successful storage. Abandoned encrypted results expire in ten minutes.

Refresh does not require a product-host user-token database. The exchange issues an HMAC-authenticated receipt bound to public key, client ID, hash of refresh token and hash of a random proof sealed to the daemon. Future refresh must present the receipt, matching refresh token and proof over TLS; the new result is sealed to that same key and keeps the nonce binding. The durable receipt signing key permits host restart without retaining user tokens. Lost signing keys force reconnect. The receipt/secret is bearer authority and belongs only in the daemon vault, never browser storage or logs.

Requests/bodies/flows are bounded, redirects are denied, and start/refresh rate limits are applied to the directly observed peer (forwarded headers are not trusted). There is no CORS API allowance. Deploy behind an ingress with global abuse limits and query/body/header logging disabled for these routes. Do not log cookies, authorization headers, vendor errors containing credentials, codes or state. Pending flows are in-memory and intentionally fail on restart; no long-term token persistence occurs product-side. The initial component serializes its exchange operations under one lock, so it is suitable for a bounded initial deployment; capacity and concurrency must be reviewed before a high-volume launch. It is not a claim of independently audited production security.

Validation covers original-daemon-only decryption, cookie/PKCE/redirect binding, callback replay, denial/expiry, fixed provider destination, unauthorized polling, receipt/token/proof mismatch, refresh after host restart, daemon identity verification, storage without the publisher secret, refresh and local disconnect rejection. These are local protocol fixtures, not Google consent evidence.

Publisher ownership/hosting choice and real vendor consent remain required. No test changes external records, sends messages or accepts vendor agreements.

Reviewable build/config preparation (run at release engineering, not on end-user machines):

```sh
cd daemon
GOMAXPROCS=2 go build -p 1 -o /private/artifacts/mewla-google-auth ./cmd/mewla-google-auth
# Supply the five host environment variables above through the chosen host's
# secret/config facility. Keep the listener private behind its TLS ingress.
# Then build official daemons with public values only:
GOMAXPROCS=2 go build -p 1 -ldflags '-X github.com/daoleno/mewla/daemon/connections.GoogleExchangeOrigin=https://CONFIRMED_OWNED_ORIGIN -X github.com/daoleno/mewla/daemon/connections.GitHubPublicClientID=REGISTERED_PUBLIC_ID -X github.com/daoleno/mewla/daemon/connections.SlackPublicClientID=REGISTERED_PUBLIC_ID' ./cmd/mewla
```

The capitalized values are required owner decisions, not working registrations. The code intentionally has no built-in fabricated defaults. Deployment must use the chosen product ingress and publisher account; this artifact does not authorize creating either.
