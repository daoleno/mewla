# Publisher setup for Plugins

**Revised 2026-10-02 after checking current public-client support.** Ordinary users install Zen and connect accounts. They never run `oauth-configure`, register an app, choose callbacks or receive Zen's shared client secret. Existing local operator configuration remains supported for that operator's own registrations; it is not the distributed product solution.

No Zen-owned GitHub/Slack/Google client or dedicated HTTPS publisher origin has been confirmed in the inspected runtime. GitHub repository ownership is not proof of ownership of other vendor accounts. No application registration or deployment is claimed.

## Smallest recommended product arrangement

| Service | End-user journey / public client capability | Publisher prerequisite | Return and credential ownership | Evidence and remaining dependency |
| --- | --- | --- | --- | --- |
| Linear | Connect → official account/workspace consent; automatic DCR, PKCE S256 | No manual app registration; daemon registers Zen as public client | Fixed `zen://plugins`; matching state/code goes through authenticated original-server channel; verifier/token remain daemon-side | Live metadata and a DCR probe accepted this URI. Real account consent remains unverified. |
| Notion | Connect → official MCP OAuth consent; advertised public DCR/PKCE | Automatic registration supported by current metadata; no manual publisher registration established as necessary | Native return only if vendor accepts it; exact MCP resource binding | Official metadata now returns200; unauthenticated Streamable HTTP returns401 with the correct resource metadata challenge. Prior403 is not evidence of redirect incompatibility. No Zen native registration or grant has been tested. |
| GitHub | Connect → official device page; code entered on GitHub, no Zen text input. Existing signed-in identity is a secondary explicitly consented import | Zen-owned OAuth app, device flow enabled; **public client ID only** shipped in official release | No redirect/hosting. Daemon polls respecting interval/slow_down; token remains in daemon vault | Device flow docs verified. No owned client ID supplied. Existing gh API can verify/import identity but is not first-time signup proof. |
| Slack | Connect → official workspace authorization with user scopes | Zen-owned app with **PKCE enabled**, `zen://plugins` registered, appropriate distribution/workspace approval; **public client ID only** shipped | Native URI + S256. `oauth.v2.access` sends verifier, no client secret. Daemon stores and rotates tokens. Public-client refresh tokens expire after 30 days | Current official PKCE guide and 2026-03-30 GA announcement confirm standard and directory apps can enable it. No Zen app registered. No live consent claimed. |
| Google Workspace | Connect → official consent. Android AuthorizationClient gives online access directly, but continued backend access requires offline authorization against a **Web client** | Zen-owned Cloud project, APIs, consent audience/verification and Web client; secret remains in a **product-owned exchange service**, never distributed | Google code callback and initial exchange hosted by product; per-flow daemon key binds encrypted delivery. Daemon vault is final token storage; refresh exchange also needs the product-held client secret | Official Android offline-access docs require backend exchange. No confirmed owned client, HTTPS origin or exchange host exists. This is the only preset needing confidential exchange in the recommended cross-platform background-access design. |
| Custom MCP / OpenAPI | Separate advanced path with service endpoint and real service-specific prerequisites | Service owner | Explicit network trust and individual grants; automatic MCP registration when supported | Existing custom functionality retained. It is not a fallback for failed built-in onboarding. |

### Notion discovery diagnosis

Rechecked2026-10-02 against the official recommended `https://mcp.notion.com/mcp` transport. Both `/.well-known/oauth-protected-resource/mcp` and the root resource metadata return200, as does `/.well-known/oauth-authorization-server`. The path-specific resource is exactly the configured MCP URL; issuer is `https://mcp.notion.com`. Metadata advertises `/register`, token authentication `none`, and PKCE `S256`. An unauthenticated MCP initialize POST returns401 and a `WWW-Authenticate` resource-metadata URL matching the path-specific document. HTTP/1.1 requests with the daemon's JSON header and Go User-Agent also succeed. A separate read-only Go probe using the same direct checked-DNS dialing, HTTP/1.1, timeout and no-redirect transport settings reproduces200/200/401, without cookies, bearer credentials, proxies or alternate service endpoints.

The earlier403 is therefore a prior discovery/transport denial that is not reproducible in this recheck. Its original cause is unproven; discovery occurs before a client or redirect is submitted, so it cannot demonstrate rejection of `zen://plugins`. The official [custom-client guide](https://developers.notion.com/guides/mcp/build-mcp-client) documents public dynamic registration and PKCE, but recommends HTTPS redirects in production and does not explicitly promise custom native schemes. Registration acceptance, official browser return and a granted read still need a later authorized check. No registration POST, consent request, credential reuse or vendor record was created during diagnosis. Do not assume another application's registered client is reusable or turn this uncertainty into an end-user callback form.

### GitHub release artifact

Register a new Zen OAuth app under the confirmed publisher account and enable device flow. Release build injects only `github.com/daoleno/zen/daemon/connections.GitHubPublicClientID` using Go `-ldflags -X`. The value is public and common to official daemon installations. It is deliberately empty until actually registered. Never reuse GitHub CLI's registered identity. GitHub `repo` scope is broad; the provider displays it and daemon permissions still separately limit reads/writes.

### Slack release artifact

Import/review `docs/plugins-slack-manifest.json` in the confirmed Slack publisher account. Enabling PKCE marks the app public and is a one-way setting except via Slack support. Register `zen://plugins`, user scopes `channels:read`, `channels:history`, `search:read`, with `chat:write` requested only for the changes group. No bot scopes on native redirects. Ship only `connections.SlackPublicClientID`; no client secret. Custom-scheme authorization always rotates tokens, even if rotation is switched off, and refresh tokens for PKCE apps expire after 30 days. Zen's adapter handles rotation and surfaces reconnect on expired authorization.

This removes the earlier proposed Slack HTTPS/static handoff. The earlier advice to disable Slack token rotation was incorrect and is superseded by this document.

### Google: minimal necessary ownership decision

Recommendation: one Google-only confidential exchange component on an **existing Zen-owned HTTPS host**, if such a host can be confirmed. Do not buy a service or register a domain merely to continue implementation. A static page alone is insufficient for a distributed Web client.

The independent implementation is now available as `daemon/googleauth`, `cmd/zen-google-auth`, and the daemon adapter `connections/google_exchange.go`. [Exact deployment configuration and binding protocol](plugins-google-exchange.md) are ready for review. It is unconfigured and undeployed.

Component responsibilities, deliberately narrower than a general OAuth platform:

1. Create a short-lived flow for a daemon-generated ephemeral encryption public key and unguessable retrieval capability. Accept a fixed Google provider/scopes/registered callback only. Return an authorization-start URL to the native app through its current-server channel. No caller-supplied daemon URL or callback.
2. Bind the browser start and callback using an HttpOnly/Secure SameSite cookie plus unpredictable single-use state. Generate PKCE at the product exchange. Callback exchanges the code with Google's client secret entirely on the product host.
3. Encrypt the token result to that flow's original daemon public key using maintained authenticated public-key encryption. Browser returns only a flow completion signal to fixed `zen://plugins`; it never receives a Google token, refresh token or retrieval capability. Daemon polls using its capability and decrypts only its own result. Ten-minute expiry, one-use completion, cancel/replay rejection and bounded capacity/rate limiting are required.
4. Daemon verifies Google identity/scopes, saves tokens in its existing vault, and exposes only an account projection to the phone. Server switch cannot reroute the flow. The exchange must support refresh with its product-held client secret; refresh requests must prove the daemon binding established at initial exchange. No long-term token database is required on the product host. Disconnection locally disables first, revokes at Google and removes local credentials/binding.
5. Publisher handles the Cloud project, APIs, consent branding/audience/test users, privacy disclosures and restricted Gmail-scope verification/security assessment. Register exactly one HTTPS callback. Backend logging must exclude request bodies, code/state queries, secrets and tokens.

Alternatives considered:

| Option | What it solves | Why selected / not selected |
| --- | --- | --- |
| Official native Google SDKs on both platforms | Online authorization on phone; Android SDK can request backend auth code | Prefer for phone-only access, but online token handoff alone does not support a daemon working after the phone leaves. Android offline access still uses a Web-client backend exchange. iOS custom scheme does not establish Android parity. |
| Existing maintained managed connection service (e.g. Nango) | OAuth orchestration/refresh and token custody | Can reduce implementation burden if Zen already owns such a service; none confirmed. Adds third-party credential custody, service/account terms and possible cost. Do not provision without a concrete owner choice. |
| Minimal Google-only exchange on an existing owned host | Keeps the one confidential secret product-side and returns credentials only to original daemon | Recommended if a suitable owned host exists; reusable protocol/design preparation is independent of registration. Production deployment needs confirmation of host/account and security review. |
| Static code handoff + client secret on every daemon | Mobile return only | Rejected: exposes shared product secret, fails no-configuration distribution, and does not solve refresh ownership. |
| Google TV/device flow or desktop client identity used for phones | Would avoid a browser callback | Rejected: cannot assume eligibility or reuse a different app type for Workspace/mobile access. |

**Single request for Brain's decision after independent preparation:** confirm the Zen publisher accounts for GitHub/Slack/Google and whether an existing owned HTTPS host is available for the Google-only exchange. If no host exists, choose between explicitly provisioning a minimal owned host or adopting an already-approved managed provider. No end-user secret entry, disabled tile or mock connection counts as completion while these publisher prerequisites are absent.

## Decisive flow changes

| Before | After | Why |
| --- | --- | --- |
| Add plugin → Connect account → nickname/token/client/callback form | Select service → meaningful permission groups → official Connect | Service is the plugin; app registration belongs to product. |
| Pending account rows created before consent | Expiring connection separate from account catalog | Cancel, deny and retry do not accumulate unusable accounts. |
| Raw methods, schemas, switches/history lead detail | Useful capabilities first; accounts, permissions and diagnostics in separate detail | Users can understand what they connected. |
| Browser return needs refresh/reachable private daemon | Fixed native callback with original-flow/current-server binding, automatic status checks | No daemon exposure or callback typing. |
| GitHub token entry | Preview verified existing identity and explicitly consent to import; official device flow for first-time accounts | Existing reuse is separate from first-time signup. |

Sources checked 2026-10-02: [Slack PKCE](https://docs.slack.dev/authentication/using-pkce), [Slack PKCE GA](https://docs.slack.dev/changelog/2026/03/30/pkce), [Slack token API](https://docs.slack.dev/reference/methods/oauth.v2.access), [Google Android authorization](https://developer.android.com/identity/authorization), [Google installed-app constraints](https://developers.google.com/identity/protocols/oauth2/native-app), [GitHub device flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow), [Linear MCP](https://linear.app/docs/mcp), [Notion MCP](https://developers.notion.com/guides/mcp/get-started-with-mcp), [Claude connector journey](https://support.claude.com/en/articles/11175166-getting-started-with-custom-connectors-using-remote-mcp).
