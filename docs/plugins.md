# Plugins

Plugins give Brain and delegated Workers access to external services through the current Zen server. Add a plugin, connect a named account once, and use its tools independently of the selected model or executor. An account belongs to one server. Switching the current server clears the account form and details; accounts are never aggregated across servers.

Open **Plugins** in the drawer or Settings. Each plugin groups its accounts, discovered tools and the last 20 calls. **Connect account** verifies identity or completes remote MCP discovery before reporting Connected. Google, Notion, Linear and remote MCP prefer system-browser OAuth. Token setup remains available for GitHub/Slack and as an alternative for Notion/Linear/custom services. An OpenAPI document alone reports **Not verified** until a permitted remote call succeeds. Connected is the last observed status, with a verification timestamp, not a continuous health guarantee.

## Initial catalog

| Plugin | Available path | Coverage |
| --- | --- | --- |
| GitHub | Fine-grained access token, or explicit `gh` import | Identity, repositories, issues/PRs, create issue, comment |
| Notion | Official remote MCP with browser OAuth; internal token alternative | Dynamic official MCP tools; token path provides workspace identity, search, page/block reads and updates |
| Google Workspace | User-owned web OAuth client; browser authorization, refresh and revocation | Drive file listing/metadata/update, Gmail list/read/send, Calendar list/events/create/update, filtered by granted scopes |
| Slack | Authorized Slack user/bot token; tools filtered by actual scopes | Identity, channel list/history, user-only search, send/update message |
| Linear | Browser OAuth, or API key/bearer token to official remote MCP | Dynamically discovered tools, individually enabled |
| Remote MCP | Streamable HTTP, browser OAuth or optional bearer token | Dynamically discovered tools through the official Go MCP SDK |
| OpenAPI | Explicit HTTPS endpoint plus OpenAPI 3.0 JSON document, optional bearer token | Operation discovery and scalar path/query plus JSON body execution |

Google is a functional connector whose server authorization may be **unconfigured**. Configure a user-owned **Web application** OAuth client in Google Cloud, enable Drive/Gmail/Calendar APIs, configure the consent audience/test users, and register the exact public HTTPS URL `https://YOUR_DAEMON_ORIGIN/plugins/oauth/callback`. Enter the client ID/secret and callback once in Plugins → Google Workspace → Connect account → Server authorization setup. The callback must reach this daemon from the phone's system browser; a private Zen pairing link is not an OAuth redirect URI. Local HTTP loopback callbacks are supported for owned desktop protocol tests, not as mobile localhost redirects.

The daemon creates a ten-minute, single-use state and PKCE S256 verifier, exchanges the browser authorization code, stores access/refresh tokens in its vault, and refreshes before expiry using maintained `golang.org/x/oauth2`. Browser responses contain no tokens or codes and offer an **Open Plugins** link. Native return refreshes account status. A daemon restart cancels pending authorization flows but preserves completed accounts and refresh credentials. Disconnect disables the account before attempting provider revocation; if remote revocation fails, retry credential removal from the account page. Providers without a revocation endpoint require vendor-side revocation separately.

Google requests identity and read scopes by default. **Allow updates** additionally requests `drive.file`, `gmail.send` and `calendar.events`; all write tools still start disabled. Drive updates are limited by Google's `drive.file` access to files authorized for this app. Granted token scopes filter tools. Gmail read scopes are restricted and may require Google verification/security assessment for an externally distributed app; testing/user-owned configurations still need the appropriate consent audience and enabled APIs. No OAuth app is registered by Zen on the user's behalf.

Generic MCP OAuth implements protected-resource discovery (including a 401 `resource_metadata` challenge), exact resource/issuer validation, authorization-server metadata, PKCE, dynamic client registration when advertised, preregistered clients, refresh and revocation. Configure the daemon callback once for the chosen plugin. A client ID is optional when the provider supports dynamic registration; otherwise supply the provider-issued client ID/secret. Dynamic registration occurs only as part of the user's Connect account action. Resource tokens never travel to discovery or registration endpoints. MCP account identity is labelled unavailable because the protocol does not define user/workspace identity.

Official Notion documentation confirms its hosted `https://mcp.notion.com/mcp` endpoint and browser OAuth login without a manual internal token in supported clients. This environment's requests to that endpoint return HTTP403 (edge error1010), so live metadata/registration compatibility is not claimed. The generic standards flow is verified against an actual local OAuth/MCP server; Notion live consent remains required. Linear's public metadata was read successfully and advertises dynamic registration, PKCE S256, refresh and revocation. No external client was dynamically registered during tests.

Linear’s official documentation also supports a bearer token or API key at `https://mcp.linear.app/mcp`; Zen reuses the remote MCP adapter for this preset. Create a scoped key at <https://linear.app/settings/account/security> and connect the intended named account. MCP has no standard user/workspace identity field, so Zen labels that identity as unavailable rather than inventing an account name. Tools remain disabled until explicitly granted.

Slack uses the official Web API. Account verification reads `auth.test` and its `X-OAuth-Scopes` header. Search is advertised only for a user account with `search:read`; bot accounts never gain search from a read-only annotation. Message writes start disabled. Use a workspace-approved Slack app token from <https://api.slack.com/apps>; this build does not register an OAuth app or obtain workspace admin consent for you.

For the optional Notion token path, create an internal connection at <https://www.notion.so/profile/integrations>, select its capabilities, then use the token in **Plugins → Notion → Connect account** on the intended server. Share the relevant pages with that connection in Notion. Host assistant/ChatGPT app permissions are not imported into Zen. The current Notion adapter uses the supported `2022-06-28` API version for its page/block operations; data-source/database querying is outside this initial tool set.

## Using tools from Brain and Workers

The daemon owns one catalog and credential vault. Both agent roles use the private local control socket:

```sh
zen connections list --json
zen connections search --query github --json
zen connections describe --id ACCOUNT_ID --tool get_me --json
zen connections invoke --id ACCOUNT_ID --tool get_me --args '{}' --json
```

Search returns at most 30 summaries without schemas. Describe returns one input schema. Invoke always names an account ID and tool. HTTP tools accept `path`, `query` and `body` objects when described by their schema. Use `--args-file` for larger inputs. Server OAuth configuration and custom network trust are available from the CLI:

```sh
zen connections oauth-configure --integration google --oauth-config-file /private/google-client.json
zen connections oauth-start --integration google --name Personal
zen connections add --integration openapi --name Local --endpoint http://127.0.0.1:PORT --trust-networks 127.0.0.1/32 --spec-file /private/api.json --credential-stdin
```

The private OAuth JSON contains `client_id`, `client_secret` (when required), and `redirect_url`. A preregistered custom MCP client must also name its exact `resource_url`; it cannot be reused for a different MCP source. Callback-only dynamic registration creates a separate client for each account/source. OAuth start returns a temporary authorization URL for the system browser; do not put it in persistent logs.

Every call, including a shell or code-mode batch, re-enters the daemon's account/policy/schema check. Zen does not inject whole tool catalogs into model prompts or own the surrounding provider's code-mode host.

`zen connections import-gh --name Personal` explicitly imports the `github.com` credential through `gh auth token`; it does not scan credential files. This copies a snapshot: later `gh` account changes do not retarget that Zen account. Token entry uses a hidden terminal prompt, or `--credential-stdin` for a secure pipe. Never place credentials in command arguments, prompts, logs or documents. The local control socket has the same trusted-OS-user authority as existing Zen CLI operations; it is not a sandbox separating agents running as that user.

## Permissions and lifecycle

Reviewed built-in read tools are enabled after account verification. Writes and all custom MCP/OpenAPI tools start disabled. Enable a specific tool for a specific account in its tool list after reviewing its purpose. This grants future calls without repetitive prompts. Tool enablement does not replace the user's authority for the current task: sending mail/messages or creating/updating external records still requires an authorized task. Remote annotations and HTTP methods do not grant access. MCP tool-definition changes invalidate grants, including at invocation time.

Disable blocks execution without deleting the credential. Disconnect commits a disabled tombstone, attempts OAuth revocation when the provider exposes it, and removes the stored credential. Failed revocation or credential removal remains disabled with a retry action. Manually supplied tokens and providers without a revocation endpoint require revocation in the service’s settings when needed. Reconnecting creates a new account identity, leaving the old account's recent history available. Revocation is linearized with execution: an already running remote call finishes before a local disconnect returns. A timed-out write may have completed remotely; inspect the service before retrying. Calls are not retried automatically.

Credentials use Zen's existing private-file vault convention: a separate `integration-credentials.json` with mode `0600` inside the daemon's private state directory. `integrations.json` contains account metadata, schemas, definition-bound grants and bounded history, never tokens. These are server runtime files and must not be committed. History contains tool names, timestamps and concise success/error status; it does not retain arguments or remote result bodies.

Requests have a 20-second deadline, inputs a 64-KiB limit, remote results a 1-MiB limit, schemas a 32-KiB limit per tool, and discovery a 200-tool limit. MCP discovery allows at most ten pages. HTTP pagination is explicit, one page per call. Custom endpoints default to HTTPS/public addresses; credentials never follow redirects. For a self-hosted MCP or internal API, explicitly enable **Trust an internal endpoint** and enter the smallest required private/loopback/Tailscale CIDRs (up to eight). That grant belongs to this account and its exact selected origin. It cannot be supplied or widened by an invocation. HTTPS hostnames retain normal certificate checks; plain HTTP is accepted only for a literal internal IP inside the explicit range. Link-local/cloud metadata ranges and public/overbroad CIDRs are rejected. OAuth internal endpoints must stay on that selected origin; a separate internal authorization origin is not implicitly trusted. The production dialer binds DNS validation to the actual dial, so a public-only account cannot rebind into an internal address. Document references remain local and redirects remain denied even for a trusted account. Uploaded OpenAPI references stay inside the document and have depth/expansion limits; files and remote `$ref` targets are rejected. Complex parameter encodings, multipart, recursive schemas and GraphQL are not supported by this first adapter.

## Architecture decision

Executor's MIT licence allows reuse. Its current `@executor-js/sdk` (source package version 1.6.10) exposes Promise APIs for `createExecutor`, `tools.list` and `tools.invoke`, with Effect-based plugins. The local CLI runs a separate Node 20+ HTTP service with its own storage, secrets and agent configuration. Embedding it would add a second service/runtime and an authority synchronization boundary to Zen's Go daemon.

This implementation therefore keeps catalog/credentials/policies in Go and reuses `github.com/modelcontextprotocol/go-sdk` for MCP, `kin-openapi` for OpenAPI parsing, and `google/jsonschema-go` for invocation validation. The bounded experiment exercised real SDK Streamable HTTP discovery/invocation with an owned MCP fixture and checked schema-change denial. GitHub, Notion and Slack use small, reviewed mappings to maintained official HTTP endpoints. This avoids a wholesale Executor fork and vendor SDK replication while keeping the existing native and CLI ownership boundary.

Sources inspected: <https://executor.sh/docs/llms.txt>, <https://executor.sh/docs/local/cli>, <https://github.com/UsefulSoftwareCo/executor/blob/main/LICENSE>, <https://github.com/UsefulSoftwareCo/executor/tree/main/packages/core/sdk>, <https://docs.slack.dev/reference/methods/search.messages/>, <https://developers.google.com/identity/protocols/oauth2/scopes>, <https://developers.google.com/workspace/gmail/api/auth/scopes>, <https://developers.google.com/identity/protocols/oauth2/native-app>, <https://linear.app/docs/mcp>, <https://developers.notion.com/docs/get-started-with-mcp>.

## Loading and verification

A watched source checkout cannot safely load new Go code while its `zen-dev` orchestration must remain running. Build a candidate from the repository root with `GOMAXPROCS=2 GOFLAGS=-p=1 ./scripts/build-zen-local.sh /owned/path/zen` in an isolated checkout. Load it only in an agreed daemon loading window. Stopping `zen-dev` normally also stops its daemon child; it is not a way to detach the watcher while keeping orchestration alive. Do not restart the user's daemon as part of UI verification.

`TestPluginsOwnedRuntime` is an opt-in, loopback-only test host using production pairing, WebSocket dispatch, catalog and CLI handlers in an explicit disposable state directory. It does not bootstrap Brain, schedulers or host discovery. `ZEN_PLUGINS_RUNTIME_DIR` enables it; run with `-count=1` so Go does not reuse a cached test result; its pairing link remains in that private directory. Ordinary tests do not contact vendors. Use serial bounded verification on shared hosts: `GOMAXPROCS=2 go test -p 1 ./...`, then each Expo platform separately with `NODE_OPTIONS=--max-old-space-size=2048` and `--max-workers 1`; run an emulator only after bundling finishes.
