# Plugins

Choose **Plugins**, select the service, review what Brain may do, and connect your account in the service's official browser authorization. There is no separate install state. Ordinary built-in onboarding has no account-name, token, client, callback, endpoint, schema or network-range field. Connected accounts belong to the canonical current server; switching servers clears the visible account and never forwards another server's callback.

The catalog names the current server and its connection state. It lists **Connected** accounts first, each with its status. **Add a service** lists the reviewed services, with a count of accounts already connected. A connected service shows its account, then **What Brain can do**, with the state of “Read and search” and “Make changes when asked”. An account that isn't working shows one recovery action: **Check** for a failed call, **Reconnect** when authorization is required, **Resume** when it is disabled, and **Retry** for a pending credential removal. Before connecting, the service page shows the access being granted. Read is included, and changes are an explicit switch. While connecting, two steps show progress: authorize in the browser, then verify on this server. A GitHub device code is shown in large type with **Copy code and open GitHub**.

**Permissions** controls the reviewed groups. **Connected accounts** handles multiple accounts and disconnecting the selected account. **Tools & activity** keeps individual tool permissions, schema inspection, enable/disable, status checks and recent call history. Custom MCP/OpenAPI remain under **Custom services**, where their endpoint, document and network-trust prerequisites are explicit. Disconnecting, allowing changes and allowing a tool still require confirmation.

## Authorization and publisher readiness

| Service | Built-in authorization | Current implementation boundary |
| --- | --- | --- |
| Linear | DCR + PKCE + fixed native return | Live daemon DCR accepted `mewla://plugins`; the official Approve screen opens. User approval/login still needed. |
| Notion | Official remote MCP, DCR + PKCE | Live daemon DCR accepts `mewla://plugins` and opens the official Notion login. Account consent and native return after login still need user verification. |
| GitHub | Official device authorization, or preview and explicitly import a server-signed-in identity through `gh` | Dev, local and release builds use the committed Mewla-owned public client ID. Live device-code issuance and the official login page are verified; user login/consent remains. Existing identity import is not first-time signup proof. Tools are GitHub's own official MCP server (`githubMCP` = `https://api.githubcopilot.com/mcp/`, the `mcp` driver with `TokenMCP`, like Linear): the device-flow or `gh` token is sent as the Bearer, with `X-MCP-Toolsets: githubToolsets` (explicit names, not `default`; `mcpSpec.Headers`, sent only to that exact endpoint). `Reads`/`Writes` in `catalog.go` classify exactly those 54 tools (36 reads, 18 writes); anything else GitHub adds in those toolsets has no class until reviewed. Identity is `get_me`.`login`. Accounts made with the former hand-written REST tools move at start (`moveGitHubToMCP`, no network): same vault token, endpoint rewritten, tools cleared; `Moved`/`ReadsOn` make the first list adopt silently and turn the reads back on (writes are not carried). Watch reads `search_repositories` (default branch), `actions_list`, `list_issues`, `list_pull_requests`, `list_dependabot_alerts`, `list_code_scanning_alerts`; GitHub reports no rate-limit headers over MCP. Device scopes are `read:user read:org repo`; device tokens of Mewla's own app were not exercised against the remote server (a `gh` OAuth token, the same `gho_` class, was) |
| Slack | Official public-client PKCE with user scopes and rotating tokens | Mewla's app (`A0CA39R2DS6`) lives in the Mewla workspace `T0CA7628ECC` owned by daodaoonline@gmail.com; its public client ID is in `release/plugin-publishers.json` and release CI requires it. Public distribution is on (activated 2026-10-18), so any workspace can install it. The app's sign-in returns through `https://connect.mewla.ai/plugins/return` (`connections.NativeReturnRelay`), which forwards only code, state and error to `mewla://plugins`, exactly like Cloudflare. `mewla://plugins` stays registered for daemons from 0.2.26 and earlier, which redirect there directly. No shared client secret or public daemon callback. |
| Hyperliquid | No provider sign-in. Public info reads, with the address as a per-call argument, and L1 signing with an API wallet key (`daemon/connections/hyperliquid*.go`) | `Manager.EnsureDefaults` (called once from `cmd/mewla/main.go`, no network call) keeps one keyless mainnet reader on, which a mainnet key upgrades in place so Brain never sees two read sets. The mainnet account refuses `disconnect` (it is switched off, or loses its key with `remove_key`); a reader that an earlier version let the person disconnect is revived in place, on, at the next start. A key is the only input: `userRole` maps the signer to its master user (verified live: `{"role":"agent","data":{"user":…}}`), mainnet first then testnet, so the key names its account and network; `user`, `vault`, `subAccount` and `missing` are refused in a line. Then `extraAgents` must list it unexpired. Tool definitions are unchanged, so existing accounts keep their grants. The key is stored per account in the vault. Signing is a Go port of the official Python SDK (`signing.py`: msgpack action hash, phantom agent, EIP-712 `Exchange` domain, source `a`/`b`) and is tested against vectors the SDK itself produced (`testdata/hyperliquid_vectors.json`, regenerated with the SDK under `python -I`). Only `order`, `batchModify`, `cancel`, `cancelByCloid`, `updateLeverage` and `scheduleCancel` are signed; user-signed actions (`withdraw3`, `usdSend`, `spotSend`, `sendAsset`) and `vaultTransfer` have no code path. |
| AWS | The installed AWS CLI; local profiles are the source of truth | Always `aws login --remote --profile`, one copy-code flow. `mewla aws` passes literal argv through read/execute authorization gates. No AWS MCP or DCR. |
| Cloudflare | Official remote MCP, DCR + PKCE | DCR accepts only https and loopback returns. The web UI registers its own `/plugins/oauth/callback`; the app registers Mewla's return page `https://connect.mewla.ai/plugins/return` (`connections.NativeReturnRelay`, served by `cmd/mewla-account`), which forwards `code`/`state`/`error`/`iss` to `mewla://plugins`. The daemon redeems with that registered return and the PKCE verifier it holds. |
| Tailscale | OAuth client (client credentials), native `tailscale` driver | No third-party OAuth exists across tailnets. The vault holds `ID:SECRET`; `tailscaleToken` mints one-hour tokens against `api.tailscale.com/api/v2/oauth/token` and caches them in memory. Tools `search`/`read`/`execute` are driven by the official OpenAPI document (`tailscale_spec.go`, cached in the state dir, 7-day refresh); only operations in it can be called, with `{tailnet}` defaulting to `-`. Connect (`tailscale_connect.go`) runs `tailscale_console.js` as one `browser_run_code_unsafe` call through `mewla agent-use chrome-mcp` (`tailscale_browser.go`), in the background and outside the manager lock; any failure falls back to the pasted secret. The console script follows the real form (checked on the Mac 2026-11-06 with a read-only inventory): re-check it with `mewla connections connect --integration tailscale --dry-run` (`chromeTools.Probe`, which stops before Generate credential) when Tailscale's page changes. |

Google is gone (user decision 2026-10-27: Mewla will not pay for the yearly CASA assessment Gmail's restricted scopes need, and Google Calendar was removed with it). The old Google Workspace account (`google`) and the later `google-calendar` and `gmail` plugins are retired: at start `removeRetired` posts each stored token to the account's revoke URL (best effort, 5 s), then deletes the account and its key. The `daemon/googleauth` exchange and its `/google/*` routes in `mewla-account` are deleted. fal and Image generation were deleted (user decision 2026-10-18, no compatibility): `removeRetired` in `daemon/connections/manager.go` deletes their accounts and vault keys when the daemon starts. Onlora covers generation.

**The product is not fully ready for first-time connection of every preset until the publisher dependencies are completed.** The daemon reports sign-in readiness in its catalog. Missing registrations show **Not yet available** with a short reason and no connection action. Existing accounts remain accessible. The daemon also rejects stale connection attempts before creating authorization state. Exact source evidence, public-client constraints, provisioning manifest and the smallest remaining publisher decision are in [Publisher setup](plugins-publisher-setup.md). End users do not perform those steps.

GitHub's device flow opens the official GitHub page, where the user enters GitHub's verification code. Mewla provides a copy action and automatically polls with the vendor's interval and slow-down instructions. Tokens remain daemon-side. The secondary import action first shows a verified identity; only the subsequent explicit Connect copies that account credential. Duplicate taps and repeated import of the same active identity reuse the account.

Linear/Notion/Slack use a fixed native return. The daemon retains the verifier and unpredictable ten-minute state. The app securely saves only the pending flow and, briefly, a returned code, bound to the original server; it completes through that server's authenticated connection. State, exact callback, resource (for MCP), issuer where supplied, expiration, and one-use completion are checked. A switched server cannot complete another flow. A daemon restart invalidates unfinished flows without creating persisted pending-account rows. Completed accounts and refreshed credentials persist.

Native authorization-session close and provider denial have explicit retry states. When the app uses an external browser, closing that browser cannot report a cancellation to Mewla; the waiting screen keeps Open authorization and Cancel connection available until completion or expiry. App resume and connection recovery recheck automatically; no Refresh is needed to finish consent. Expired and cancelled flows cannot execute. A network failure after a native return can retry verification with the same bound return. GitHub device consent may complete after the browser closes; the waiting screen offers a clear cancel action.

Slack public clients request only user scopes on the relay redirect and send S256/verifier without `client_secret`. The official token response has a nested `authed_user` on initial consent and top-level rotating token on refresh; both are handled. The refreshed token pair is saved before subsequent use. Slack's public-client refresh tokens expire after 30 days; expired authorization requires reconnect.

## The integration catalog

Everything that differs per service lives in one typed table, `daemon/connections/catalog.go`, validated when the daemon starts (a malformed entry stops it). An entry holds the service's name and setup link, its fixed API origin, whether a credential is required, how a person signs in (publisher, registered or owner-supplied OAuth client; fixed or discovered endpoints; scopes; device flow), the reviewed built-in tools or the official MCP read/write allowlist with its exact endpoint, the headers and query defaults the network layer adds, and the write scopes a grant needs before changes can be allowed. Generic code reads the entry and never asks which service this is.

Behaviour that is code (Hyperliquid signing and adding, Slack's token exchange, refresh and revoke, MCP and OpenAPI discovery) is registered once as a driver or token codec and named by the entry that uses it. Adding a service is adding one entry, plus a driver or codec only when the existing ones do not fit.

The trust model is not data in the entries: the MCP allowlist is fail-closed and applies only on its exact endpoint, an unreviewed tool belongs to no group, and every grant is bound to the full tool-definition fingerprint. `daemon/connections/testdata/golden_integrations.json` pins every service's catalog row, readiness, tool classes, endpoints, scopes and sign-in requests; a change to it needs a deliberate `MEWLA_UPDATE_GOLDEN=1` regeneration.

## Permissions and execution

Reviewed built-in read groups become usable after successful identity verification/discovery. Optional write consent grants the reviewed changes group, not arbitrary tools or remote annotations. Notion and Linear allowlists are tied to their fixed official resource URLs. Unknown newly discovered tools require individual review. Custom MCP/OpenAPI receive no automatic group grants. All grants remain bound to complete tool-definition fingerprints; MCP rediscovery at invocation rejects changed schemas. Vendor scopes further limit advertised operations.

Permission does not replace the user's instruction for a task. Sending mail/messages and creating/updating external records still require that task's authority. Reads are bounded, one page/call by default. Search exposes summaries, Describe exposes one schema, and Invoke names the account/tool explicitly:

```sh
mewla connections list --json
mewla connections search --query github --json
mewla connections describe --id ACCOUNT_ID --json   # every tool, name and description
mewla connections describe --id ACCOUNT_ID --tool get_me --json
mewla connections invoke --id ACCOUNT_ID --tool add_asset --args-file asset.json --json
```

Disconnect commits the disabled state before provider revocation and credential deletion. Future calls fail even if removal/revocation needs retry. Providers without supported revocation still require vendor-side removal if complete vendor revocation is desired. Calls already in flight finish before disconnect returns. Writes are never automatically retried.

Credentials live in the daemon's private 0600 vault. Account metadata/history contain no token, arguments or result bodies. Existing linked accounts remain usable. A verified reconnect can retain the stable known identity's account ID. Where the vendor does not expose a supported identity, Mewla says so instead of requiring a nickname or inventing an identity.

The existing private control CLI retains operator-owned OAuth configuration and token/custom-service functionality for advanced use. Those commands are not the end-user built-in installation journey and must never distribute Mewla's shared publisher secret.

## Custom service boundaries

Hyperliquid's form (`HyperliquidForm`) is the one built-in service with a text input: the API wallet key, on the same keyboard-aware scroll view. It is the optional **Trading** section, one **Add API wallet key** row until tapped, and shows while no account has a trading key (afterwards behind **Add another key**), and the account card carries a Testnet tag on testnet accounts (mainnet is the default and has none; the keyless reader is named `Hyperliquid`, a testnet account `Hyperliquid Testnet`).

Custom forms use the shared `react-native-keyboard-controller` keyboard-aware scroll view on Android and iOS. It follows focused inputs and keyboard geometry automatically, including lower private-network fields, with the existing `Spacing.lg` token as the supported caret clearance, without a second keyboard-padding wrapper, manual scroll coordinates or timers. The same scroll container stays mounted while fields update; built-in connection screens still contain no text inputs.

Custom endpoints use HTTPS/public addresses by default. A per-account grant permits explicitly selected internal CIDRs; cloud metadata/link-local addresses and overbroad ranges remain rejected. DNS validation is bound to dialing. Credentials never follow redirects. OAuth resource/client identity remains bound to the exact endpoint, including refresh. OpenAPI references stay local to the submitted document; file/remote references, recursive schemas and unsupported body encodings are rejected. Calls cannot override endpoint, credential, policy or schema.

The daemon uses maintained Go MCP, OAuth2, OpenAPI and JSON-schema packages. The app uses Expo WebBrowser's native authorization sessions on Android/iOS when that module is present. Older native clients open the same official authorization URL through React Native Linking instead, with the existing state/PKCE, original-server binding, deep-link handling and app-resume checks. The optional native package is loaded only after checking availability, so a JavaScript update cannot crash route discovery on an older client. A matching native build enables the integrated auth-session UI; a JavaScript update cannot add that native module. Android and iOS bundle checks are required; device evidence must identify its actual platform.

## Verification and loading

Run meaningful connection regressions, `GOMAXPROCS=2 go test -p 1 ./...`, daemon build and app TypeScript. Export Android and iOS serially with `NODE_OPTIONS=--max-old-space-size=2048`, Metro `--max-workers 1`. Build the normal matching native package; start only one small emulator after bundling exits. Protocol fixtures prove protocol behavior, not real vendor consent.

`TestPluginsOwnedRuntime` provides disposable production pairing/WebSocket/control dispatch without starting Brain or schedulers. Keep its state private and remove it after verification. A live `mewla-dev` watcher can restart the Brain host when original Go source changes; build in isolation, then use the current private checkpoint/recovery loading procedure to integrate and load safely. Never reset the original provider, tmux or state.

### CLI cloud accounts

AWS uses the managed official AWS CLI and the person's ~/.aws config, credentials
and caches. Lists return an immutable snapshot without acquiring the account
mutation lock or starting a CLI. Per-provider background refreshes are deduplicated
for one minute, with a shared four-command limit. Discovery publishes rows before
status checks finish; `connections_changed` pushes snapshots to paired clients,
which ignore updates from any other server. `checking` is transient presentation
state. Refreshes merge only records unchanged since their snapshot, so deletes
and permission changes win over stale work. Newly discovered profiles are disabled for agents until
the person enables them. Add and Sign in again always run `aws login --remote`
with a named profile, regardless of where the browser runs. Mewla validates the
returned code/state before feeding the CLI; the CLI owns PKCE and credential storage.
Login processes expire after ten minutes and cancellation reaps the child.

`mewla aws [--profile NAME] <AWS args>` runs literal argv, never a shell. AWS `get-`, `list-`, and `describe-` operations across all services run as reads, plus `s3 ls` and S3 head operations. An explicit deny list keeps credential/secret readers and local object downloads gated. SSM parameter reads require authorization with `--with-decryption` (including abbreviations) or structured CLI input. Everything else needs enabled execute
and task-specific `--mewla-authorize`; secret-returning operations are excluded
from reads. The paired API carries the same argv using `cloud_run`. No credentials,
endpoint overrides, local file loaders or sign-in management through this tool.
JSON is the default; the CLI output is transported unchanged and never logged.
Because ~/.aws is shared, this is an agent authorization boundary, not an OS
sandbox against a local process invoking bare aws or reading its owner's files.

Deletion requires confirmation naming the profile/account. Config and credentials
are backed up with private permissions before changes; only matching sections are
removed atomically. AWS logout clears a login cache only if no other profile uses
that login_session. Shared sessions and SSO caches remain for the other profiles.
CLI deletions disappear from Mewla on the next refresh. Retired AWS OAuth records
and their vault credentials are removed, with no compatibility path.

`cloudProvider` in cloud_cli.go defines the next gcloud adapter boundary: executable,
environment, remote login argv, URL/code validation, command classification and
installer. cloudRuntime owns execution limits and cloudLogin owns process lifetime.
Provider profile enumeration/deletion and identity discovery remain vendor specific.
Managed binaries take precedence over PATH. Update attempts are recorded under
`cli/<provider>/update.json` at most once every 24 hours, including failures and
daemon restarts. An hourly scheduler checks providers already used; first command
use and a seven-day gap wait for the shared update (bounded to three minutes).
The installer lock also prevents two processes from updating the same directory.
A failed update preserves the active generation and falls back to a validated
local executable when needed (AWS >=2.32, gcloud >=400).

AWS verifies the official detached PGP signature on Linux and AWS's Apple team
signature on macOS. ETag plus the installed version avoids unchanged downloads;
version and model-loading smoke checks run before activation. Google bootstraps
from a SHA256-pinned archive, then uses `gcloud components update --quiet` in an
independent SDK copy. Google's HTTPS rapid-channel manifest and component updater
provide new versions and checksum verification without refreshing Mewla's bootstrap
pins on every SDK release. As a Mewla release maintenance step, check Google's
[versioned archives](https://docs.cloud.google.com/sdk/docs/downloads-versioned-archives)
and refresh `gcpBootstrapVersion` plus all four `gcpArchives` SHA256 digests
together when the bootstrap is superseded; run the installer fault tests and an
official package smoke test. The bootstrap is not the automatic update source. Installer settings are isolated from the person's
gcloud config; ordinary command settings remain shared. Both installers reject
downgrades and activate with an atomic symlink rename only after validation.
Commands resolve the launcher and acquire a generation lease under the same
filesystem gate used for activation/pruning. The lease is held for the entire
command or interactive login and inherited by the child process on Linux/macOS.
Each provider keeps the active generation and its recorded predecessor; pruning
deletes older generations only when it can acquire their lease exclusively.
A candidate's predecessor is published atomically with its launcher, so failed
activation cannot alter the current rollback pair. Installers also lease staging
directories. Maintenance runs on first provider use after daemon start and after
update attempts (including no-op/offline checks); held generations are reconsidered
on the next pass after their command exits.

`cli/backups` contains profile-mutation snapshots, not SDK packages. Keep the
two newest directories plus any backup held by an active mutation. Creation and
pruning share a filesystem gate; the mutation retains its lease through rollback
and failures. Backup release immediately re-runs pruning. Credential/config
files outside Mewla's backup directory are not part of this maintenance.

`catalog[].cli` projects the selected version and managed/local source from the
background refresh, never from a synchronous list-time process. `--version` is
the only account-free `cloud_run` probe; cloud command authorization is unchanged.

An OAuth callback can land on a different Mewla computer using the same loopback
port. Unknown state must never redirect away or exchange the code: the callback
page retains its original URL and provides a full-address Copy action. The page
uses no-store, no-referrer and a nonce-bound script; callback values are not
rendered into HTML or logged. Known states retain their single-use behavior.
The computers channel currently transports durable Brain messages, not an OAuth
state-owner lookup or ephemeral credential handoff. Do not send authorization
codes through that message history; use the copy page until an authenticated
ownership protocol exists. Macs running released code need the next daemon
release containing this fix (after 0.2.88); updating zen alone cannot change a
callback already handled by a Mac daemon.

Cloud profiles share a compact account list: the service row summarizes sign-in counts, valid profiles precede unchecked ones, and expired/invalid profiles are collapsed. Selecting a row reveals its existing permissions and delete confirmation. Filtering appears above eight profiles and matches profile, account identity, alias and project. Sign-in status is independent of whether agents have access. Add/sign-in stays above the list.

AWS profile names are the actual CLI section names. After sign-in the completion card suggests account/role naming; Rename in an account's detail uses `cloud_rename` with `profile`, validates a unique CLI-safe name, backs up config and credentials, atomically replaces each file (rolling back on failure), updates `source_profile` references and carries agent/write grants to the new profile ID. A terminal rename appears on the next refresh; as a new external profile it starts with agents disabled.

Sign-in timing never uses rotating AWS login credentials or ID-token iat/exp. After successful Mewla remote sign-in and identity verification, persist its completion time and a digest of the cached ID token's session_arn. For that same session, show completion + 12h as an explicit upper bound (the principal/console session can end sooner), with exact local time in detail. Refresh does not extend it; unknown terminal sessions show no countdown. Legacy nonrefreshable SSO uses the matching cache expiresAt; modern sso-session tokens also refresh, so no session deadline is inferred. Only CLI identity/refresh failure marks Expired; passing a timestamp alone never does. No token/key is returned or copied. Gcloud user credentials have no invented sign-in expiry.

Sources: [AWS CLI login session limits](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sign-in.html), [SSO token-provider and legacy session behavior](https://docs.aws.amazon.com/sdkref/latest/guide/feature-sso-credentials.html).

## Google Workspace (gws)

`gwsCLI` is a `cloudProvider` for Google's `gws` with one profile, `default`; its
account is `gws auth status` (user, token_valid), so an outside sign-out removes the
card. `gws` loads a `.env` from its working directory or any parent and never
overrides a variable that is already set, so it runs in an empty owner-only directory
under the state dir (`cli/gws/run`) with the token, credentials, client, project,
config-dir and keyring variables pinned explicitly (blank, or the resolved value).
The pinned release (`gwsVersion`, `gwsArchives` SHA256s, refreshed with the gcloud
bootstrap pins) is extracted in-process and published only after `--version` matches.

Connect has two provider hooks beyond gcloud's. `setupFlow` runs before sign-in:
without `client_secret.json` the flow is `setup_required` (the code field takes the
client), and `gws auth setup` is not used because it cannot create the OAuth client
(its consent-screen call returns 404 and client creation was removed). With exactly
one connected and enabled gcloud account Connect runs `gcloud projects create` and
`services enable` for it; it never picks between accounts. `finishSetup` writes the
client where `gws auth status` says `client_config` lives. Sign-in uses `gws auth login -s
gmail,calendar,drive` (gmail.modify, calendar, drive): `gws` listens on a random
loopback port and has no copy-code mode, so the flow is `address_required` and
`deliverCode` replays only `code` and `scope` from the pasted address, and only to
127.0.0.1 on the port in the printed redirect URI, once.

Reads are an allowlist (`classify`): last path word `list`, `get` or `getX`,
`freebusy query`, `+triage`, `+read`, `+agenda`, `schema`. Everything else is a
change. File loaders (`--upload`, `--output`, `--attach`, `@file`), watch/push and
`auth` are refused. The 7-day Testing-mode refresh-token expiry is avoided by
publishing the app in the console; that step is documented, not verified by code.

