# OpenCode Local Usage

Mewla Stats aggregates OpenCode model usage from OpenCode 2's background
service. There is no subscription probing, no credential read, no
OpenCode-internal configuration, and no upstream API access. Mewla supports
OpenCode 2 only.

## Data source

OpenCode 2 keeps sessions behind a background service and documents its API as
OpenAPI. Stats and Work reach it with the official client, `opencode api
<operation>`, which finds and authenticates to the running service itself
(`daemon/opencodeapi`). Mewla never reads the service password or OpenCode's
storage tables.

- `session.list` pages sessions newest-updated first.
- `session.message.list --param type=assistant` returns each session's
  assistant messages. Each records the usage fields Mewla aggregates:

```
model.{id,providerID}, cost, tokens.{input,output,reasoning,cache.{read,write}},
time.created
```

The session's `location.directory` attributes usage to a project. A session's
messages are re-read only when its `time.updated` changes. The database file
that `opencode debug paths` reports is only stat'ed, as a cheap change stamp
for live conversations.

When the service is not running or `opencode` is missing, OpenCode usage is
absent, never estimated.

## Aggregation

Only observed facts are aggregated, keyed by the `(provider, model)` pair
recorded in each message:

- **Request count**: one per assistant message that carries token usage.
- **Tokens**: input, output, reasoning, cache read, and cache write summed
  exactly as recorded.
- **Cost**: the recorded `cost` value. A model with messages that lack a cost
  is reported with cost *unknown*; cost is never estimated or invented for
  OpenCode models.
- Messages without any token usage (for example interrupted or empty turns)
  and user messages contribute nothing.
- Missing token subfields contribute zero to their bucket; unavailable
  metrics are omitted.
- Quota, subscription, plan, or provider-entitlement state is never inferred.

The aggregated rows flow into the same per-date model, project, and activity
aggregates as Claude Code, Codex, Grok, and Cursor Agent usage. With no
OpenCode usage, nothing OpenCode-specific appears.

## Security

- Every call is a read (`session.list`, `session.message.list`); Mewla never
  writes through the API for Stats.
- No credentials are read, logged, or serialized. No upstream requests are
  made.
