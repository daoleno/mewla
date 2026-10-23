# Commands

Everything the app does goes through the daemon, and most of it can be done or
inspected from the computer with `mewla`. Commands that talk to a running
daemon reach it through its local control socket; nothing here needs the
network. Most accept `--json` for scripts. `mewla <command> --help` prints the
flags for that command.

## Run the daemon

```sh
mewla                         # start on 127.0.0.1:9876 (same as mewla serve)
mewla --lan                   # also listen on Wi-Fi/LAN and Tailscale, for a trusted network
mewla -addr "$(tailscale ip -4):9876"   # listen on host:port only, here your Tailscale IP
mewla doctor [--json]         # is this machine ready? tmux, state, port, agents
mewla deps [-only tmux|agent] # offer this system's official tmux / agent CLI install, then sign-in
mewla setup                   # guided first run: Brain's agent and the safe profile
mewla update [--check]        # verify and install the latest release
mewla version [--json]        # version, commit and platform
mewla boot install|status|uninstall   # start at login: systemd user service (Linux), LaunchAgent (macOS)
```

`-state-dir DIR` selects a state directory other than `~/.mewla`. Pass the same
value to every command that should use it.

## Pair and manage devices

```sh
mewla web [--print]           # open the web UI in this computer's browser, paired; print its addresses
mewla pair [address]          # one-time code: phone QR and a short link for this computer's browser
mewla pair --details          # the same code as paste links for phone and every browser address
mewla address list [--json]   # every address phones and browsers can use
mewla address add <https-url> # an HTTPS tunnel or proxy in front of this computer
mewla address remove <address>
mewla devices list [--json]
mewla devices pending         # browsers waiting for approval
mewla devices approve -id <id> -number <shown-number>
mewla devices deny -id <id> -number <shown-number>
mewla devices revoke -id <device-id>
```

See [Connect and pair](connect-and-pair.md).

## Brain and Work

```sh
mewla brain executors --json          # Brain's agent and the agents available
mewla brain use <executor>            # move Brain to another agent; the thread stays
mewla brain workspace                 # path to Brain's notes
mewla brain objective show|clear
mewla brain objective set "Ship atlas-notes v1.4 this week"   # the goal line in the app
mewla brain replies set -row "Yes|No" [-push "one line"]      # one line of tap-to-answer chips after Brain's last message
mewla brain work list --json          # open Work; -all, -full, -id <work> for more (-id adds transcript paths)
mewla memory search "<phrase>" [--from user|brain|context|all] [--since 30d] [--limit 20] [--json]
mewla memory show <id> [--around 3] [--json]   # a chat message with its neighbours
mewla brain work update -id <work> -status done
mewla brain work update -id <work> -status waiting \
  -question "Keep both copies, or the newest edit?" -choice "Keep both" -choice "Newest wins"
```

See [Brain and Work](brain-and-work.md).

## Workers

```sh
mewla worker list --json
mewla worker status -id <session> --json
mewla worker capture -id <session> --json     # the transcript
mewla worker send -id <session> --work-id <work> -text "Also update the tests"
mewla worker receipt -id <session> --work-id <work> --turn-id <turn>   # did an input arrive?
mewla worker approvals [-id <session>]        # what a Claude Worker is waiting for you to allow
mewla worker approve -id <session> -approval <id> -option allow   # or deny
mewla worker close -id <session>
```

`approvals` and `approve` answer the permission prompts of a Claude or Codex
Worker that Mewla started and that is not in bypass mode. The answer goes
through Claude's own hook or Codex's app-server, so no keys are sent; with no
answer the agent's own prompt decides. The phone answers the same requests.

`mewla worker spawn`, `worker progress`, `worker release`, `brain context`,
`brain playbooks`, `brain gc` and the other `brain work` actions (`create`,
`close`, `event`, `resolve`) are what Brain and its Workers call. You can run
them, but you rarely need to.

## Services, Calendar, Screen, Telegram, Plugins, Resources, Watch

```sh
mewla service list --json
mewla service register -unit my-preview.service -name "Docs preview" -port 3080
mewla service unregister -unit my-preview.service
mewla service tunnel start|status|stop -id <service> -generation <generation>

mewla calendar list --json
mewla calendar create -title "Review plan" -kind reminder -date 2026-10-09 -time 09:30
mewla calendar run -id <item> --json
mewla calendar cancel -id <item> -revision <revision> --json

mewla screen status           # support, permissions, allowed devices, keep-awake
mewla screen disable          # turn Screen off; every device must be allowed again
mewla screen enable [-allow-sleep]

mewla agent-use status|on|off|fix      # agents use your Chrome; the app's one switch
mewla computers list|status            # other Mewla computers this Brain can hand work to
mewla computers send mac "…"           # hand a task (or an answer) to that computer's Brain
mewla computers invite | add <invite>  # connect two computers without the app
mewla computers remove mac

mewla telegram setup          # asks for the bot token, prints a one-time link to bind your chat
mewla telegram status|enable|disable

mewla connections list --json
mewla connections import-gh   # copy your current gh login into the daemon's vault

mewla resources               # one resource sample as JSON

mewla watch status            # signals per source and project, last pass, errors
mewla watch projects
mewla watch signals --project mewla --source human --since 24h
mewla watch problems --rule judgment
mewla watch enable && mewla watch judge   # agent judgment; costs tokens
mewla watch problem label <id> real|not|later --note "why"   # later takes --for 1d|3d|2w (default 7d)
mewla watch problem link <id> --work <work id>   # Work that handles it: fixing, then fixed
mewla watch triage [--backlog]     # hand Brain the new problems; --backlog sends every waiting one
mewla watch brief                  # hand Brain the morning brief now
mewla watch project rename --project <id> --name "New name"
mewla watch project link-linear --project <id> --team KEY --confirm
```

Details: [Services](sessions.md#services), [Calendar](calendar.md),
[Telegram](notifications.md#telegram), [Plugins](plugins.md),
[Resources](usage-and-resources.md#resources), [Watch](watch.md).
