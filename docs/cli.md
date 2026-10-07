# Commands

Everything the app does goes through the daemon, and most of it can be done or
inspected from the computer with `mewla`. Commands that talk to a running
daemon reach it through its local control socket; nothing here needs the
network. Most accept `--json` for scripts. `mewla <command> --help` prints the
flags for that command.

## Run the daemon

```sh
mewla                         # start on 127.0.0.1:9876 (same as mewla serve)
mewla --lan                   # listen on every IPv4 interface, for a trusted network
mewla -addr "$(tailscale ip -4):9876"   # listen on one address, here your Tailscale IP
mewla -web-origin https://mewla.example.com   # serve the web UI behind your HTTPS proxy
mewla doctor [--json]         # is this machine ready? tmux, state, port, agents
mewla setup                   # guided first run: Brain's agent and the safe profile
mewla update [--check]        # verify and install the latest release
mewla boot install|status|uninstall   # run as a user systemd service (Linux)
```

`-state-dir DIR` selects a state directory other than `~/.mewla`. Pass the same
value to every command that should use it.

## Pair and manage devices

```sh
mewla pair [https://origin]   # a fresh one-time pairing link and QR code
mewla web [-no-open]          # open the web UI on this computer, paired as a new browser
mewla devices list [--json]
mewla devices pending         # browsers waiting for approval
mewla devices approve -id <id> -number <shown-number>
mewla devices deny -id <id>
mewla devices revoke -id <device-id>
mewla address add|remove <https-url>   # addresses the app may use to reach this daemon
mewla address list [--json]
```

See [Connect and pair](connect-and-pair.md).

## Brain and Work

```sh
mewla brain executors --json          # Brain's agent and the agents available
mewla brain use <executor>            # move Brain to another agent; the thread stays
mewla brain workspace                 # path to Brain's notes
mewla brain objective show|clear
mewla brain objective set "Ship atlas-notes v1.4 this week"   # the goal line in the app
mewla brain work list --json          # open Work; -all, -full, -id <work> for more
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
mewla worker close -id <session>
```

`mewla worker spawn`, `worker progress`, `worker release`, `brain context`,
`brain playbooks`, `brain gc` and the other `brain work` actions (`create`,
`close`, `event`, `resolve`) are what Brain and its Workers call. You can run
them, but you rarely need to.

## Services, Calendar, Telegram, Plugins, Resources

```sh
mewla service list --json
mewla service register -unit my-preview.service -name "Docs preview" -port 3080
mewla service unregister -unit my-preview.service
mewla service tunnel start|status|stop -id <service> -generation <generation>

mewla calendar list --json
mewla calendar create -title "Review plan" -kind reminder -date 2026-10-09 -time 09:30
mewla calendar run -id <item> --json
mewla calendar cancel -id <item> -revision <revision> --json

mewla telegram setup          # asks for the bot token, prints a one-time link to bind your chat
mewla telegram status|enable|disable

mewla connections list --json
mewla connections import-gh   # copy your current gh login into the daemon's vault

mewla resources               # one resource sample as JSON
```

Details: [Services](sessions.md#services), [Calendar](calendar.md),
[Telegram](notifications.md#telegram), [Plugins](plugins.md),
[Resources](usage-and-resources.md#resources).
