# Plugins

Choose **Plugins**, select the service, review what Brain may do, and connect your account in the service's official browser authorization. There is no separate install state. Ordinary built-in onboarding has no account-name, token, client, callback, endpoint, schema or network-range field. Connected accounts belong to the canonical current server; switching servers clears the visible account and never forwards another server's callback.

The connected service shows useful capabilities first. **Permissions** controls reviewed “Read and search” and “Make changes when asked” groups. **Connected accounts** contains multiple-account connection and disconnect. **Tools & activity** retains individual tool permissions, schema inspection, enable/disable, status checks and recent call history. Custom MCP/OpenAPI remain under **Custom services**, where their real endpoint, document and network-trust prerequisites are explicit.

## Authorization and publisher readiness

| Service | Built-in authorization | Current implementation boundary |
| --- | --- | --- |
| Linear | DCR + PKCE + fixed native return | Live DCR accepted `zen://plugins`; account consent still needed. |
| Notion | Official remote MCP, DCR + PKCE + native return | This environment returns HTTP403 from vendor metadata; live compatibility is unverified. |
| GitHub | Official device authorization, or preview and explicitly import a server-signed-in identity through `gh` | First-time device flow needs a real Zen-owned public client ID in the release. Existing identity import is not first-time signup proof. |
| Slack | Official public-client PKCE with user scopes and rotating tokens | Requires Zen publisher registration with PKCE enabled and a public client ID in the release. No shared client secret or public daemon callback. |
| Google Workspace | Existing operator-owned Web OAuth works on an already configured daemon | The prepared Google-only exchange and daemon adapter support distributed no-configuration background access once publisher hosting/registration is provisioned. No end-user configuration workaround is offered. |

**The product is not fully ready for first-time connection of every preset until the publisher dependencies are completed.** Missing registrations are explicit errors, never token-entry fallbacks or simulated Connected states. Exact source evidence, public-client constraints, provisioning manifest and the smallest remaining publisher decision are in [Publisher setup](plugins-publisher-setup.md). End users do not perform those steps.

GitHub's device flow opens the official GitHub page, where the user enters GitHub's verification code. Zen provides a copy action and automatically polls with the vendor's interval and slow-down instructions. Tokens remain daemon-side. The secondary import action first shows a verified identity; only the subsequent explicit Connect copies that account credential. Duplicate taps and repeated import of the same active identity reuse the account.

Linear/Notion/Slack use a fixed native return. The daemon retains the verifier and unpredictable ten-minute state. The app securely saves only the pending flow and, briefly, a returned code, bound to the original server; it completes through that server's authenticated connection. State, exact callback, resource (for MCP), issuer where supplied, expiration, and one-use completion are checked. A switched server cannot complete another flow. A daemon restart invalidates unfinished flows without creating persisted pending-account rows. Completed accounts and refreshed credentials persist.

Browser close and denial have explicit retry states. App resume and connection recovery recheck automatically; no Refresh is needed to finish consent. Expired and cancelled flows cannot execute. A network failure after a native return can retry verification with the same bound return. GitHub device consent may complete after the browser closes; the waiting screen offers a clear cancel action.

Slack public clients request only user scopes on the native redirect and send S256/verifier without `client_secret`. The official token response has a nested `authed_user` on initial consent and top-level rotating token on refresh; both are handled. The refreshed token pair is saved before subsequent use. Slack's public-client refresh tokens expire after 30 days; expired authorization requires reconnect.

## Permissions and execution

Reviewed built-in read groups become usable after successful identity verification/discovery. Optional write consent grants the reviewed changes group, not arbitrary tools or remote annotations. Notion and Linear allowlists are tied to their fixed official resource URLs. Unknown newly discovered tools require individual review. Custom MCP/OpenAPI receive no automatic group grants. All grants remain bound to complete tool-definition fingerprints; MCP rediscovery at invocation rejects changed schemas. Vendor scopes further limit advertised operations.

Permission does not replace the user's instruction for a task. Sending mail/messages and creating/updating external records still require that task's authority. Reads are bounded, one page/call by default. Search exposes summaries, Describe exposes one schema, and Invoke names the account/tool explicitly:

```sh
zen connections list --json
zen connections search --query github --json
zen connections describe --id ACCOUNT_ID --tool get_me --json
zen connections invoke --id ACCOUNT_ID --tool get_me --args '{}' --json
```

Disconnect commits the disabled state before provider revocation and credential deletion. Future calls fail even if removal/revocation needs retry. Providers without supported revocation still require vendor-side removal if complete vendor revocation is desired. Calls already in flight finish before disconnect returns. Writes are never automatically retried.

Credentials live in the daemon's private 0600 vault. Account metadata/history contain no token, arguments or result bodies. Existing linked accounts remain usable. A verified reconnect can retain the stable known identity's account ID. Where the vendor does not expose a supported identity, Zen says so instead of requiring a nickname or inventing an identity.

The existing private control CLI retains operator-owned OAuth configuration and token/custom-service functionality for advanced use. Those commands are not the end-user built-in installation journey and must never distribute Zen's shared publisher secret.

## Custom service boundaries

Custom endpoints use HTTPS/public addresses by default. A per-account grant permits explicitly selected internal CIDRs; cloud metadata/link-local addresses and overbroad ranges remain rejected. DNS validation is bound to dialing. Credentials never follow redirects. OAuth resource/client identity remains bound to the exact endpoint, including refresh. OpenAPI references stay local to the submitted document; file/remote references, recursive schemas and unsupported body encodings are rejected. Calls cannot override endpoint, credential, policy or schema.

The daemon uses maintained Go MCP, OAuth2, OpenAPI and JSON-schema packages. The app uses Expo WebBrowser's native authorization sessions on Android/iOS; a newly built native package is required for this dependency. A JavaScript-only OTA cannot add the native module. Android and iOS bundle checks are required; device evidence must identify its actual platform.

## Verification and loading

Run meaningful connection regressions, `GOMAXPROCS=2 go test -p 1 ./...`, daemon build and app TypeScript. Export Android and iOS serially with `NODE_OPTIONS=--max-old-space-size=2048`, Metro `--max-workers 1`. Build the normal matching native package; start only one small emulator after bundling exits. Protocol fixtures prove protocol behavior, not real vendor consent.

`TestPluginsOwnedRuntime` provides disposable production pairing/WebSocket/control dispatch without starting Brain or schedulers. Keep its state private and remove it after verification. A live `zen-dev` watcher can restart the Brain host when original Go source changes; build in isolation, then use the current private checkpoint/recovery loading procedure to integrate and load safely. Never reset the original provider, tmux or state.
