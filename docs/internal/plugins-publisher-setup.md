# Publisher setup for Plugins

**Revised 2026-10-02 after checking current public-client support.** Ordinary users install Zen and connect accounts. They never run `oauth-configure`, register an app, choose callbacks or receive Zen's shared client secret. Existing local operator configuration remains supported for that operator's own registrations; it is not the distributed product solution.

The committed GitHub publisher client is configured. Slack registration and Google publisher hosting/registration remain pending. GitHub repository ownership is not proof of ownership of other vendor accounts. No publisher-owned application registration or deployment is claimed; automatic protocol DCR is distinguished below.

## Smallest recommended product arrangement

| Service | End-user journey / public client capability | Publisher prerequisite | Return and credential ownership | Evidence and remaining dependency |
| --- | --- | --- | --- | --- |
| Linear | Connect → official account/workspace consent; automatic DCR, PKCE S256 | No manual app registration; daemon registers Zen as public client | Fixed `zen://plugins`; matching state/code goes through authenticated original-server channel; verifier/token remain daemon-side | Live metadata and a DCR probe accepted this URI. Real account consent remains unverified. |
| Notion | Connect → official MCP OAuth consent; advertised public DCR/PKCE | Automatic registration supported by current metadata; no manual publisher registration established as necessary | Fixed native URI accepted by real DCR; exact MCP resource binding | Official metadata now returns200; unauthenticated Streamable HTTP returns401 with the correct resource metadata challenge. Prior403 is not evidence of redirect incompatibility. Actual Zen DCR accepted `zen://plugins`, S256 and the exact MCP resource; official browser login loaded. Account grant/read/native return remain unverified. |
| GitHub | Connect → official device page; code entered on GitHub, no Zen text input. Existing signed-in identity is a secondary explicitly consented import | Zen-owned OAuth app, device flow enabled; **public client ID only** shipped in official release | No redirect/hosting. Daemon polls respecting interval/slow_down; token remains in daemon vault | Device flow docs verified. The public client ID is committed in `release/plugin-publishers.json`. Existing gh API can verify/import identity but is not first-time signup proof. |
| Slack | Connect → official workspace authorization with user scopes | Zen-owned app with **PKCE enabled**, `zen://plugins` registered, appropriate distribution/workspace approval; **public client ID only** shipped | Native URI + S256. `oauth.v2.access` sends verifier, no client secret. Daemon stores and rotates tokens. Public-client refresh tokens expire after 30 days | Current official PKCE guide and 2026-03-30 GA announcement confirm standard and directory apps can enable it. No Zen app registered. No live consent claimed. |
| Google Workspace | Connect → official consent. Android AuthorizationClient gives online access directly, but continued backend access requires offline authorization against a **Web client** | Zen-owned Cloud project, APIs, consent audience/verification and Web client; secret remains in a **product-owned exchange service**, never distributed | Google code callback and initial exchange hosted by product; per-flow daemon key binds encrypted delivery. Daemon vault is final token storage; refresh exchange also needs the product-held client secret | Official Android offline-access docs require backend exchange. No confirmed owned client, HTTPS origin or exchange host exists. A confidential product-side exchange is needed for this offline cross-platform design; component selection follows the maintained-option comparison below. |
| Custom MCP / OpenAPI | Separate advanced path with service endpoint and real service-specific prerequisites | Service owner | Explicit network trust and individual grants; automatic MCP registration when supported | Existing custom functionality retained. It is not a fallback for failed built-in onboarding. |

### Notion discovery diagnosis

Rechecked2026-10-02 against the official recommended `https://mcp.notion.com/mcp` transport. Both `/.well-known/oauth-protected-resource/mcp` and the root resource metadata return200, as does `/.well-known/oauth-authorization-server`. The path-specific resource is exactly the configured MCP URL; issuer is `https://mcp.notion.com`. Metadata advertises `/register`, token authentication `none`, and PKCE `S256`. An unauthenticated MCP initialize POST returns401 and a `WWW-Authenticate` resource-metadata URL matching the path-specific document. HTTP/1.1 requests with the daemon's JSON header and Go User-Agent also succeed. A separate read-only Go probe using the same direct checked-DNS dialing, HTTP/1.1, timeout and no-redirect transport settings reproduces200/200/401, without cookies, bearer credentials, proxies or alternate service endpoints.

The earlier403 is therefore a prior discovery/transport denial that is not reproducible in this recheck. Its original cause is unproven; discovery occurs before a client or redirect is submitted, so it cannot demonstrate rejection of `zen://plugins`. The official [custom-client guide](https://developers.notion.com/guides/mcp/build-mcp-client) documents public dynamic registration and PKCE, but recommends HTTPS redirects in production and does not explicitly promise custom native schemes. Registration acceptance, official browser return and a granted read still need a later authorized check. No registration POST, consent request, credential reuse or vendor record was created during diagnosis. Do not assume another application's registered client is reusable or turn this uncertainty into an end-user callback form.

### GitHub: approved owner and concrete registration handoff

Approved owner is **daoleno**, not the currently accessible CLI identity `paul-freeride`. Never switch that production identity or register under it as a shortcut. If an accessible signed-in publisher browser is unavailable, the owner can complete the following in their own browser; no password/session/secret transfer is needed:

1. Check [existing OAuth apps](https://github.com/settings/developers) while signed in as daoleno. Reuse an appropriate Zen registration if one exists.
2. Otherwise open [Register a new OAuth application](https://github.com/settings/applications/new):

| Field | Truthful value |
| --- | --- |
| Application name | Zen |
| Homepage URL | `https://github.com/daoleno/zen` |
| Description | Mobile-native control plane for coding agents. Connect GitHub accounts with user authorization. |
| Authorization callback URL | `http://127.0.0.1/` |
| Enable Device Flow | Enabled |
| Expire user access tokens | Keep enabled |
| Owner | daoleno |

The form's loopback callback is unused by device flow. GitHub supports literal loopback callbacks; this does not require a daemon listener or hosted callback. Do not invent a Zen domain/privacy policy to fill a new requirement. Return **only the public Client ID**, never a client secret or token.

Product release engineering stores it in `release/plugin-publishers.json`. `scripts/plugin-publisher-flags.py` validates public-only fields and the approved owner, and emits Go linker flags consumed by both `build-zen-local.sh` and all three `build-daemon-linux.sh` targets. `go run ./cmd/zen-dev` runs the same validator on every daemon rebuild; missing/invalid configuration fails the rebuild. Restart the dev runner after updating its own source. Pending IDs may remain null and appear unavailable in the app. The release workflow requires a real configured GitHub ID before building/publishing release artifacts; no public release/tag is created by this preparation. Unknown/secret fields and unsafe linker characters are rejected. End users do not run `oauth-configure`.

GitHub now defaults new OAuth apps to expiring tokens. Device-issued refresh uses the public Client ID without a secret, as documented by GitHub. The daemon must retain expiry and rotating refresh tokens in its vault and show reconnect when the grant expires. GitHub `repo` is a broad vendor scope; daemon read/write capability enforcement remains separate. Existing signed-in import is a secondary flow and does not prove first-time device authorization.

### Slack release artifact

Import/review `docs/internal/plugins-slack-manifest.json` in the confirmed Slack publisher account. Enabling PKCE marks the app public and is a one-way setting except via Slack support. Register `zen://plugins`, user scopes `channels:read`, `channels:history`, `search:read`, with `chat:write` requested only for the changes group. No bot scopes on native redirects. Ship only `connections.SlackPublicClientID`; no client secret. Custom-scheme authorization always rotates tokens, even if rotation is switched off, and refresh tokens for PKCE apps expire after 30 days. Zen's adapter handles rotation and surfaces reconnect on expired authorization.

This removes the earlier proposed Slack HTTPS/static handoff. The earlier advice to disable Slack token rotation was incorrect and is superseded by this document.

### Google: evaluate maintained components before selecting

The approved sequence is GitHub, Linear/Notion, Slack, then Google. [Maintained-component comparison](plugins-google-options.md) checks current licenses, free/paid features, credentials, deployment and the mobile-to-offline-daemon boundary. The prepared [Google-only exchange](plugins-google-exchange.md) is a candidate, not a selected/deployed service.

The minimum remaining Google owner decision is an actually owned Cloud project and an existing owned HTTPS host **after reviewing that comparison**. No paid plan, domain purchase, new host or vendor/legal acceptance is implied. All approaches still require real Google branding/audience/scopes/verification. Shared client secrets may never be shipped to daemons. A static handoff alone cannot solve that boundary.

## Decisive flow changes

| Before | After | Why |
| --- | --- | --- |
| Add plugin → Connect account → nickname/token/client/callback form | Select service → meaningful permission groups → official Connect | Service is the plugin; app registration belongs to product. |
| Pending account rows created before consent | Expiring connection separate from account catalog | Cancel, deny and retry do not accumulate unusable accounts. |
| Raw methods, schemas, switches/history lead detail | Useful capabilities first; accounts, permissions and diagnostics in separate detail | Users can understand what they connected. |
| Browser return needs refresh/reachable private daemon | Fixed native callback with original-flow/current-server binding, automatic status checks | No daemon exposure or callback typing. |
| GitHub token entry | Preview verified existing identity and explicitly consent to import; official device flow for first-time accounts | Existing reuse is separate from first-time signup. |

Sources checked 2026-10-02: [Slack PKCE](https://docs.slack.dev/authentication/using-pkce), [Slack PKCE GA](https://docs.slack.dev/changelog/2026/03/30/pkce), [Slack token API](https://docs.slack.dev/reference/methods/oauth.v2.access), [Google Android authorization](https://developer.android.com/identity/authorization), [Google installed-app constraints](https://developers.google.com/identity/protocols/oauth2/native-app), [GitHub device flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow), [Linear MCP](https://linear.app/docs/mcp), [Notion MCP](https://developers.notion.com/guides/mcp/get-started-with-mcp), [Claude connector journey](https://support.claude.com/en/articles/11175166-getting-started-with-custom-connectors-using-remote-mcp).

## Registration handoff (2026-10-05)

### Slack: five steps

1. Open <https://api.slack.com/apps>, **Create New App → From a manifest**.
2. Select the publisher workspace, then **Next**.
3. Select **JSON**, paste [plugins-slack-manifest.json](plugins-slack-manifest.json), then **Next**.
4. Review and **Create**. In **OAuth & Permissions**, verify PKCE, `zen://plugins`, and user scopes `channels:read`, `channels:history`, `search:read`, `chat:write`. No bot scopes. The app requests `chat:write` only when the user enables changes. PKCE is a one-way setting; native tokens rotate and refresh authorization expires after 30 days.
5. Copy **Basic Information → App Credentials → Client ID** and the selected workspace's `T…` ID. Set the `slack` object in `release/plugin-publishers.json` to `{"workspace_id":"T…","client_id":"…"}`. Rebuild/restart the daemon. No client secret is needed. Cross-workspace installation additionally requires Slack's distribution approval/settings.

### Google: personal daemon registration

This configures an operator's own app on their own daemon; it does not provision a shared Zen publisher service.

1. In <https://console.cloud.google.com/>, select/create the owner's project. Under **APIs & Services → Library**, enable **Google Drive API**, **Gmail API**, and **Google Calendar API**.
2. Open **Google Auth Platform → Branding → Get started**. Supply app name, support email and developer contact. Choose **Audience → External → Testing**, and add the account(s) under **Test users** (up to 100). An eligible Workspace-only project may choose Internal instead. Testing refresh tokens normally expire after seven days for these scopes, requiring reconnect.
3. Under **Data Access → Add or remove scopes**, add `openid`, `email`, `https://www.googleapis.com/auth/drive.readonly`, `https://www.googleapis.com/auth/gmail.readonly`, and `https://www.googleapis.com/auth/calendar.readonly`. Optional changes additionally use `https://www.googleapis.com/auth/drive.file`, `https://www.googleapis.com/auth/gmail.send`, and `https://www.googleapis.com/auth/calendar.events`.
4. Under **Clients → Create client → Web application**, add the daemon's exact public HTTPS origin followed by `/plugins/oauth/callback` as an **Authorized redirect URI**. No JavaScript origin is required for this server-side flow. Download the credentials privately. Google does not accept `zen://plugins` for this Web client.
5. Store a private mode-0600 JSON file containing `client_id`, `client_secret`, and that exact `redirect_url`. Apply it with one command: `zen connections oauth-configure --integration google --oauth-config-file /private/google-oauth.json`. The running daemon immediately reports Google available; no rebuild is needed. Delete the staging file after successful application. The secret stays on that operator's daemon and must never enter the publisher manifest or app bundle.

For external production distribution, publishing the consent screen is separate from verification. These sensitive/restricted scopes require Google's review; server-side use of restricted Gmail/Drive data may also require a security assessment. Supply the actual owned domain, privacy policy and required verification evidence. Do not promise that switching from Testing to Production completes verification. A shared Zen publisher deployment additionally needs the existing [Google exchange](plugins-google-exchange.md), an approved owned HTTPS origin, and its secret configuration. Client IDs alone cannot provision that service.
