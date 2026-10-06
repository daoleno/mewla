# Providers and usage

**Model Providers** decide where Codex and Claude Code send their model
requests. **Stats** shows what each model used and cost. Both read from the
current server.

## Model Providers

Open **Settings > Agents > Model Providers**. Each client (Codex, Claude Code)
has one selected connection:

| Connection | Requests go to |
| --- | --- |
| Official login | The provider directly, using the CLI's own sign-in on your computer |
| OpenAI, Anthropic, DeepSeek or OpenRouter | That provider, with the API key you saved |
| Custom Gateway | Your own endpoint, with the key you saved |

For **Official login**, sign in to each CLI on the computer with its own login
command. For the others, the API key is stored on the daemon in Zen's
credential store. The app never shows a saved key again; leaving the key field
empty keeps the existing key.

For a custom Claude gateway, enter its endpoint root, including any proxy path
and optionally ending in `/v1`.

### How routing works

Zen runs one local gateway on the computer (`127.0.0.1:3425`, or the next free
port). Every time the daemon starts it points Codex and Claude Code at that
gateway, so even a CLI you start in a plain terminal or an IDE uses the selected
connection:

- **Codex**: a marked block in Codex's `config.toml`. The previous file is
  backed up first.
- **Claude Code**: `ANTHROPIC_BASE_URL` and a placeholder token in
  `~/.claude/settings.json`. Other settings are kept, and your previous values
  come back when no Claude connection is selected.

The gateway swaps the placeholder for the real key and forwards the request
unchanged. When you switch the Claude connection, running Claude Sessions use
the new one on their next request.

While the daemon is stopped, the gateway refuses connections. To use a CLI
without Zen, restore its backup or remove those two Claude settings.

### Models

Zen lists the models a connection offers, preferring the provider's live
catalog and falling back to the last good copy and local metadata. Choosing a
connection does not set a default model: with no model chosen for a Session,
each CLI keeps its own setting (for Claude Code, whatever you chose with
`/model`). A model picked for one Session applies to that Session only.

The Model Provider is separate from Brain's executor. Changing one never
changes the other; see [Agents and executors](executors.md).

## Usage

**Stats** reads the agents' own history on the computer and shows, per model,
tokens, sessions and cost for the date range you pick. Tap a model for input,
output and cache tokens, the price source and when it was last refreshed.

Costs come in four kinds, and Stats keeps them apart:

| Kind | Meaning |
| --- | --- |
| Reported | The agent's history recorded an actual charge |
| Estimated | Zen priced the tokens from a public reference price list (marked with `≈`) |
| Mixed | Part reported, part estimated |
| Unknown | No price is known; shown as a dash, never as zero |

An estimate uses public reference prices; your provider or gateway may bill
differently. Zen never guesses a price for a model that is not in the price
list.

### Notes per agent

- **Claude Code**: read from `~/.claude/projects` every five minutes. Usage of
  a Custom Gateway counts too, because Claude Code still writes its normal
  history. Costs are reference estimates, not your subscription bill, and Zen
  does not show subscription limits. History in a custom `CLAUDE_CONFIG_DIR`
  is not read.
- **Codex**: each request is priced at its own context size, so long-context
  pricing tiers apply per request rather than per day.
- **OpenCode**: read from OpenCode's local database.

Switching servers shows that server's usage only. Zen does not add up several
computers into one bill.
