# Plugins

Plugins give Brain and delegated Workers access to external services through the current Zen server. Add a plugin, connect a named account once, and use its tools independently of the selected model or executor. An account belongs to one server. Switching the current server clears the account form and details; accounts are never aggregated across servers.

Open **Plugins** in the drawer or Settings. Each plugin groups its accounts, discovered tools and the last 20 calls. **Connect account** verifies GitHub/Notion/Slack identity or completes Linear/remote MCP discovery before reporting Connected. An OpenAPI document alone reports **Not verified** until a permitted remote call succeeds. Connected is the last observed status, with a verification timestamp, not a continuous health guarantee.

## Initial catalog

| Plugin | Available path | Coverage |
| --- | --- | --- |
| GitHub | Fine-grained access token, or explicit `gh` import | Identity, repositories, issues/PRs, create issue, comment |
| Notion | Internal connection token | Bot/workspace identity, search, page/block reads, block children, update page, append blocks |
| Google Workspace | Catalog recommendation; built-in OAuth is not yet implemented | Drive/Gmail/Calendar need a configured OAuth client and user consent |
| Slack | Authorized Slack user/bot token; tools filtered by actual scopes | Identity, channel list/history, user-only search, send/update message |
| Linear | Linear API key or bearer token to official remote MCP | Dynamically discovered tools, individually enabled |
| Remote MCP | HTTPS Streamable HTTP endpoint, optional bearer token | Dynamically discovered tools through the official Go MCP SDK |
| OpenAPI | Explicit HTTPS endpoint plus OpenAPI 3.0 JSON document, optional bearer token | Operation discovery and scalar path/query plus JSON body execution |

Google requirements were checked against its current Gmail scopes and native-app OAuth documentation: Gmail read scopes are restricted, mobile loopback redirects are deprecated, and embedded WebView user agents are disallowed. A complete Google path needs a registered client, supported native/system-browser return flow, refresh-token storage and actual user/workspace consent. That path is not implemented by this release.

Catalog entries that need authorization implementation are labelled **Setup required**, with an official setup link. They are not connected accounts. Custom adapters are independent escape hatches, not claims of built-in OAuth support. No plugin marketplace, package installation or JavaScript execution runtime is introduced.

Linear’s official documentation explicitly supports a bearer token or API key at `https://mcp.linear.app/mcp`; Zen reuses the remote MCP adapter for this preset. Create a scoped key at <https://linear.app/settings/account/security> and connect the intended named account. MCP has no standard user/workspace identity field, so Zen labels that identity as unavailable rather than inventing an account name. Tools remain disabled until explicitly granted.

Slack uses the official Web API. Account verification reads `auth.test` and its `X-OAuth-Scopes` header. Search is advertised only for a user account with `search:read`; bot accounts never gain search from a read-only annotation. Message writes start disabled. Use a workspace-approved Slack app token from <https://api.slack.com/apps>; this build does not register an OAuth app or obtain workspace admin consent for you.

For Notion, create an internal connection at <https://www.notion.so/profile/integrations>, select its capabilities, then use the token in **Plugins → Notion → Connect account** on the intended server. Share the relevant pages with that connection in Notion. Host assistant/ChatGPT app permissions are not imported into Zen. The current Notion adapter uses the supported `2022-06-28` API version for its page/block operations; data-source/database querying is outside this initial tool set.

## Using tools from Brain and Workers

The daemon owns one catalog and credential vault. Both agent roles use the private local control socket:

```sh
zen connections list --json
zen connections search --query github --json
zen connections describe --id ACCOUNT_ID --tool get_me --json
zen connections invoke --id ACCOUNT_ID --tool get_me --args '{}' --json
```

Search returns at most 30 summaries without schemas. Describe returns one input schema. Invoke always names an account ID and tool. HTTP tools accept `path`, `query` and `body` objects when described by their schema. Use `--args-file` for larger inputs. Every call, including a shell or code-mode batch, re-enters the daemon's account/policy/schema check. Zen does not inject whole tool catalogs into model prompts or own the surrounding provider's code-mode host.

`zen connections import-gh --name Personal` explicitly imports the `github.com` credential through `gh auth token`; it does not scan credential files. This copies a snapshot: later `gh` account changes do not retarget that Zen account. Token entry uses a hidden terminal prompt, or `--credential-stdin` for a secure pipe. Never place credentials in command arguments, prompts, logs or documents. The local control socket has the same trusted-OS-user authority as existing Zen CLI operations; it is not a sandbox separating agents running as that user.

## Permissions and lifecycle

Reviewed built-in read tools are enabled after account verification. Writes and all custom MCP/OpenAPI tools start disabled. Enable a specific tool for a specific account in its tool list after reviewing its purpose. This grants future calls without repetitive prompts. Tool enablement does not replace the user's authority for the current task: sending mail/messages or creating/updating external records still requires an authorized task. Remote annotations and HTTP methods do not grant access. MCP tool-definition changes invalidate grants, including at invocation time.

Disable blocks execution without deleting the credential. Disconnect commits a disabled tombstone and removes the stored secret; it does not revoke the token at the vendor. Revoke vendor tokens in the service's settings when needed. Reconnecting creates a new account identity, leaving the old account's recent history available. Revocation is linearized with execution: an already running remote call finishes before a local disconnect returns. A timed-out write may have completed remotely; inspect the service before retrying. Calls are not retried automatically.

Credentials use Zen's existing private-file vault convention: a separate `integration-credentials.json` with mode `0600` inside the daemon's private state directory. `integrations.json` contains account metadata, schemas, definition-bound grants and bounded history, never tokens. These are server runtime files and must not be committed. History contains tool names, timestamps and concise success/error status; it does not retain arguments or remote result bodies.

Requests have a 20-second deadline, inputs a 64-KiB limit, remote results a 1-MiB limit, schemas a 32-KiB limit per tool, and discovery a 200-tool limit. MCP discovery allows at most ten pages. HTTP pagination is explicit, one page per call. Custom endpoints require HTTPS; credentials never follow redirects. The production dialer rejects private/loopback addresses and binds DNS validation to the actual dial. Uploaded OpenAPI references stay inside the document and have depth/expansion limits; files and remote `$ref` targets are rejected. Complex parameter encodings, multipart, recursive schemas, automatic OAuth refresh and GraphQL are not supported by this first adapter.

## Architecture decision

Executor's MIT licence allows reuse. Its current `@executor-js/sdk` (source package version 1.6.10) exposes Promise APIs for `createExecutor`, `tools.list` and `tools.invoke`, with Effect-based plugins. The local CLI runs a separate Node 20+ HTTP service with its own storage, secrets and agent configuration. Embedding it would add a second service/runtime and an authority synchronization boundary to Zen's Go daemon.

This implementation therefore keeps catalog/credentials/policies in Go and reuses `github.com/modelcontextprotocol/go-sdk` for MCP, `kin-openapi` for OpenAPI parsing, and `google/jsonschema-go` for invocation validation. The bounded experiment exercised real SDK Streamable HTTP discovery/invocation with an owned MCP fixture and checked schema-change denial. GitHub, Notion and Slack use small, reviewed mappings to maintained official HTTP endpoints. This avoids a wholesale Executor fork and vendor SDK replication while keeping the existing native and CLI ownership boundary.

Sources inspected: <https://executor.sh/docs/llms.txt>, <https://executor.sh/docs/local/cli>, <https://github.com/UsefulSoftwareCo/executor/blob/main/LICENSE>, <https://github.com/UsefulSoftwareCo/executor/tree/main/packages/core/sdk>, <https://docs.slack.dev/reference/methods/search.messages/>, <https://developers.google.com/identity/protocols/oauth2/scopes>, <https://developers.google.com/workspace/gmail/api/auth/scopes>, <https://developers.google.com/identity/protocols/oauth2/native-app>, <https://linear.app/docs/mcp>.

## Loading and verification

A watched source checkout cannot safely load new Go code while its `zen-dev` orchestration must remain running. Build a candidate from the repository root with `GOMAXPROCS=2 GOFLAGS=-p=1 ./scripts/build-zen-local.sh /owned/path/zen` in an isolated checkout. Load it only in an agreed daemon loading window. Stopping `zen-dev` normally also stops its daemon child; it is not a way to detach the watcher while keeping orchestration alive. Do not restart the user's daemon as part of UI verification.

`TestPluginsOwnedRuntime` is an opt-in, loopback-only test host using production pairing, WebSocket dispatch, catalog and CLI handlers in an explicit disposable state directory. It does not bootstrap Brain, schedulers or host discovery. `ZEN_PLUGINS_RUNTIME_DIR` enables it; its pairing link remains in that private directory. Ordinary tests do not contact vendors. Use serial bounded verification on shared hosts: `GOMAXPROCS=2 go test -p 1 ./...`, then each Expo platform separately with `NODE_OPTIONS=--max-old-space-size=2048` and `--max-workers 1`; run an emulator only after bundling finishes.
