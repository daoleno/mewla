# Usage And Pricing

Stats reads supported agent history from the current server. Switching servers
rebinds the data; Mewla does not aggregate unrelated servers into one bill.

Reported charges remain reported charges. When a transcript supplies usage but
not a charge, Mewla can estimate from a reference catalog. The screen distinguishes
reported, estimated, mixed, and unknown costs, and preserves exact model/provider
identities. A gateway's actual billing may differ from a public reference tariff.

The model overview shows one name/amount line and one tokens/sessions line.
Amounts containing reference estimates use an approximation mark; missing
amounts remain a dash. Provider names appear only to distinguish identical model
IDs. Tap a model for its full selectable name, exact amount, mixed-cost breakdown,
source, refresh time, token details, and any missing-price reason. Summary totals
remain visible at the top; model diagnostics are not repeated in the list.

Provider model discovery and newly observed usage request an asynchronous catalog
refresh. Requests are bounded, repeated requests coalesce, failed refreshes back
off, and the last successful prices remain available. Changed rates recalculate
existing usage. Source, refresh time, stale state, and refresh failure remain
visible in Stats.

For Codex, matching `last_token_usage` and cumulative deltas retain the input
context of each request (including cached input) before daily aggregation.
Supported catalog context tiers are applied to those requests, not to the sum
of a day's tokens. Explicit typed context thresholds take precedence over the
catalog's legacy `context_over_200k` alias. Duplicate token reports do not add
cost. Missing or mismatched request evidence remains unknown; any known portion
is still shown as a partial estimate. Price refreshes reprice retained usage.

Unknown does not mean zero:

- **Price not in catalog**: no matching reference price exists for the exact model.
- **Price found; request context unavailable**: the catalog has a price, but usage
  lacks the context needed to select a supported tier.
- **Some token rates unavailable**: the catalog does not price every reported token type.

Discovery proving that a model exists does not prove its price. In particular,
Mewla does not guess a rate for `gpt-6-astra` or substitute another model's price.
Failed catalog requests retry automatically with backoff. Refresh Stats to read
the latest collected result; it does not bypass that backoff. Missing catalog
entries remain unknown until authoritative pricing becomes available.

## Claude Code local usage

Claude Code usage is already collected from the current daemon host's
`~/.claude/projects/**/*.jsonl` history, or `$CLAUDE_CONFIG_DIR/projects` when
the daemon's environment sets it. This also works when Claude Code was
launched with a Mewla Custom Provider: the client still writes its normal native
history. No proxy management API, remote helper, extra login or quota bridge is
required. Background collection runs at startup and every five minutes; reopening
Stats reads the current cached result, rather than forcing an inference request.

In Stats, select the date range, then find the exact Claude model under **Models**
(expand the list if necessary). Tap the model for **Input**, **Output**, **Cache
read**, **Cache write**, **Tokens** and **Sessions**. Repeated streaming records
with the same assistant message ID within a transcript contribute the maximum
reported counters once; distinct messages are additive. Claude input, output,
cache-read and cache-creation tokens are separate buckets, all included in total
tokens. Sessions are counted by transcript/model per active day; multi-day ranges
sum those daily counts rather than reporting unique lifetime conversations.

These are local usage observations, not account-wide subscription utilization.
The historical JSONL records do not reliably identify a Mewla Provider connection.
Consequently, model rows can include earlier connections using the same model;
changing a Provider does not reassign or erase local history. The `anthropic`
model/reference provider identifies the pricing family, not proof of a direct
Anthropic login or the connection through which that usage was incurred. Stats
never treats a Custom Provider's downstream key as a local official Claude login.
Claude subscription limits and reset windows are not supplied by this local
source and are not inferred from token counts.

Claude JSONL token usage normally has no actual charge. Any displayed cost is an
API reference estimate, not a Claude subscription charge or the Custom Provider's
bill. Missing reference rates remain unknown while tokens remain visible.

Collection covers the one config root the daemon resolves. History under a
different per-process `CLAUDE_CONFIG_DIR`, a remote host, or a
disabled/non-persistent Claude session is not automatically discovered. Cross-file copies/forks are not globally
deduplicated. These limits are distinct from ordinary Custom Provider routing,
which does not itself prevent standard native history from being counted.
