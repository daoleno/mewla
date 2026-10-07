# Plugins

Plugins connect Brain and its Workers to services you use: Linear, Notion,
GitHub, Slack, Google Workspace, and custom MCP or OpenAPI services. Plugins
are a **preview**.

Connected accounts belong to the current server. Switching servers in the app
shows that server's accounts and never moves an account between servers.

## Connect a service

1. Open the menu and choose **Plugins**.
2. Under **Add a service**, pick the service.
3. Review what Brain may do. **Read and search** is included; **Make changes
   when asked** is a separate switch that is off by default.
4. Sign in on the service's own page in the browser, then return to Mewla, which
   verifies the account on your server.

For GitHub, Mewla shows a device code in large type; tap **Copy code and open
GitHub** and enter it on GitHub's page. You can also import the account that
the GitHub CLI (`gh`) is already signed in to on your computer; Mewla shows the
identity first and copies it only after you confirm.

There are no tokens, client IDs or callback URLs to type for built-in services.

## Sign-in availability

First-time sign-in is not yet available for every service, because some need
Mewla's own app registration with the provider:

| Service | Sign-in |
| --- | --- |
| Linear | Official browser sign-in |
| Notion | Official browser sign-in |
| GitHub | Device code, or import from `gh` |
| Slack | Shows **Not yet available** until Mewla's Slack app registration ships |
| Google Workspace | Shows **Not yet available** unless your daemon already has Google sign-in configured |

A service that cannot be connected yet shows **Not yet available** with a short
reason. Accounts you already connected keep working.

## What Brain can do

Each connected account shows **What Brain can do**:

- **Read and search**: on by default after sign-in.
- **Make changes when asked**: off until you turn it on.

**Permissions** controls these groups. **Tools & activity** lets you allow or
block single tools, inspect a tool's schema, run a status check and see recent
calls. New tools a service adds later need your review before use.

Permission is not an instruction. Even with changes allowed, Brain should send
messages or update records only when the task you gave it calls for that.
Writes are never retried automatically.

If an account stops working, it shows one recovery action: **Check** after a
failed call, **Reconnect** when sign-in expired, **Resume** when disabled, or
**Retry** when removing its credentials did not finish.

## Custom services

Under **Custom services** you can add an MCP server or an OpenAPI service by
endpoint or document. Custom endpoints must be public HTTPS by default. To reach
a service on your own network, grant that account an explicit address range;
cloud metadata and link-local addresses are always refused. Custom services get
no automatic permissions.

## Disconnect

**Connected accounts** lists every account; choose one and disconnect it. Mewla
disables the account first, so later calls fail even if revoking it at the
provider needs a retry. Some providers do not support revocation from Mewla; in
that case also remove Mewla's access in the provider's settings.

## Use plugins from the computer

Brain and Workers use the same accounts through the CLI:

```sh
mewla connections list --json
mewla connections search --query github --json
mewla connections describe --id ACCOUNT_ID --tool get_me --json
mewla connections invoke --id ACCOUNT_ID --tool get_me --args '{}' --json
```

Each call is one bounded request; nothing pages automatically.

## Where credentials live

Tokens stay on the daemon, in a private vault file readable only by your user.
Account history records which tool was called and when, but not tokens,
arguments or results.
