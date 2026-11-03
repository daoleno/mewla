# Agents outside Mewla

Claude Code, Codex or any other MCP client running in a terminal can hand work
to Brain. Brain stays the decider: what the agent sends becomes a Brain turn,
worded as coming from an outside agent rather than from you, and Brain starts
Work, answers or asks you. The agent never starts or steers a Worker itself.

## Connect an agent

On the computer where the agent runs, with Mewla running there:

```sh
mewla agents mcp connect -for claude     # or -for codex, or the default -for print
```

It prints the agent's name and a three-digit number, then waits. Approve it in
the app under **Settings > Outside agents**, where the same name and number
show, or at the terminal with `mewla agents mcp approve <number>`. A request
expires after five minutes. Once approved, `-for claude` runs
`claude mcp add` for you; `-for codex` prints the two lines Codex needs; the
default prints both. Use `-name` to say which agent or project this is.

The token stays with the agent: Mewla keeps only a hash of it. It lets that
agent hand work to Brain until you revoke it, so keep it private.

An agent on another computer uses this computer's remote access address, which
is https: `mewla agents mcp connect -url https://<your address>/mcp`. A token
is never accepted over plain HTTP from the network, over Wi-Fi/LAN or through a
proxy that forwards a public host name to this computer.

## What the agent can do

| Tool | What it does |
| --- | --- |
| `list_work` | Open Work on this computer (`all` includes finished Work) |
| `get_work` | One Work item: status, question and recent events |
| `ask_brain` | Hand Brain a request; returns a request id |
| `get_request` | Whether Brain has it, what Brain replied, and the Work it linked |
| `send_followup` | Add to or change a request already made |

Brain receives each request as an outside agent's message and is told it is not
your own words: it asks you before anything sensitive, such as paying, sending
to others, deleting or changing accounts. Brain answers with
`mewla agents mcp reply <request id> [-work <work id>] "<result>"`; the agent
reads the answer with `get_request`. Each agent can make 30 calls an hour, and
sees only its own requests.

## Manage agents

```sh
mewla agents mcp list              # approved agents and requests waiting
mewla agents mcp revoke <agent id> # its token stops working at once
mewla agents mcp requests          # what agents handed to Brain
```

The app lists the same agents under **Settings > Outside agents**, with
**Revoke** on each. See [Security and privacy](security-and-privacy.md#agents-outside-mewla).
