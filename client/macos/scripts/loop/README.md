# The warm Mac loop

Two routes to the running Mac app while you change it: a GitHub macOS runner kept warm (this directory), and a real
Mac through its own Brain (the second half, on paper until that Mac's Brain answers).

## The hosted runner (`mac-loop.sh`)

A clean macOS 26 runner builds Mewla Dev once, then holds. This computer reaches it over ssh: an sshd of the job's own,
key-only, behind a cloudflared quick tunnel that the runner opens outward. The key is made per run and only its public
half goes to GitHub. Mewla Dev runs on the runner with its own state and port and a stub `claude`.

```sh
client/macos/scripts/loop/mac-loop.sh start            # refuses beside a release or another loop; ~10 min queue + ~8 min warm (measured 11-23)
client/macos/scripts/loop/mac-loop.sh ui               # rsync, nsh build, relaunch, screenshot: ~13 s
client/macos/scripts/loop/mac-loop.sh daemon           # rsync, go build (darwin, the engine), relaunch, screenshot: ~16–23 s
client/macos/scripts/loop/mac-loop.sh start --host     # also builds the host's loop profile in the background once warm
client/macos/scripts/loop/mac-loop.sh host             # rsync, cargo --profile loop, relaunch, screenshot
client/macos/scripts/loop/mac-loop.sh shot name | log 40 | click X Y | type "text" | key 36 | ssh [cmd]
client/macos/scripts/loop/mac-loop.sh stop             # as soon as the task is done
```

State (the private key, the ssh config, the screenshots) lives in `$MAC_LOOP_DIR` (default `$TMPDIR/mac-loop`).
`MAC_LOOP_REF` dispatches another branch's copy of the workflow. The workflow file has to be on main to be dispatched
at all.

**Yielding, always.** The account gets at most 5 macOS jobs at once, often fewer in practice. On 11-23 a warm loop held
the v0.4.26 release's signing job in the queue. So:
- `start` refuses while a release run (release-artifacts, macos-release including the Mac release gate, ios-release,
  release-next-beta) or another loop is queued or running (`yield.py blockers`);
- the hold ends the run once any other macOS job in the repo has waited 2 minutes (`yield_after`) for a runner. It
  checks every minute (`yield.py waiters`), and the run's log names the job it yielded to;
- it holds 45 minutes at most (`--minutes`), one loop at a time (a concurrency group).

When a session to the runner fails mid-task, look at the run: it has most likely yielded. Start again once the queue
is clear.

**What it cannot show.** SIP is off on GitHub's Macs, so no real permission prompts appear and TCC.db is writable. A
drag into System Settings stops at the password dialog. The machine is new every run, so there is no long-lived
state. The screen is 1024×768, and the runner has 3 M1 cores and 7 GB of RAM.

**The host's loop profile.** `client/macos/Cargo.toml` `[profile.loop]` is release code without LTO, with 256 codegen
units and incremental compilation. With the shipped thin-LTO profile, a one-line host change took 307–328 s to
rebuild on the runner. The loop profile's first build is a full build of NSH (more than 11 min cold), so it starts in
the background only with `start --host`, and `host` waits for it. It is never shipped.

## A real Mac through its Brain (the owner's MacBook): the rules

This is the route for what only a real Mac shows: permission prompts and their rows, a real drag with the password
dialog, and long-lived state (the installed Mewla's job, old rows, the Keychain, real agents, a real update).

A Worker that Brain on that Mac starts for the task:
- keeps its own checkout and caches in its own prefix, never in ~/Documents, ~/Desktop or ~/Downloads (those raise
  folder prompts). The tools (Xcode command-line tools, Rust, Go, NSH), about 5 GB of caches and a few minutes of CPU
  for the first build are fine;
- builds Mewla Dev (`package.sh --dev`: com.daoleno.mewla.dev, its own state and port, its daemon its child) beside
  the installed Mewla.app, then rebuilds incrementally as above. It signs every rebuild with one fixed local identity,
  never ad hoc: a new identity loses Mewla Dev's grants and prompts on the owner's screen;
- runs Mewla Dev with `MEWLA_APP_BACKGROUND=1` (never the active app) and an inert Brain executor
  (`MEWLA_APP_BRAIN_HOST_EXECUTOR`), never the owner's `claude` login.

**Input: elements only, always.** No pointer or keyboard events at all, whether or not the owner is at the Mac. That
means no CGEvent, no System Events keystrokes, no `activate` or `frontmost`. The Worker acts through Accessibility
actions on Mewla Dev's own process (AXPress, setting AXValue), and sees through captures of Mewla Dev's windows only
(`screencapture -o -l <window id>`, capture-windows.sh), never the whole screen. It does not judge by idle time. The
click, type and key commands above are for the hosted runner only.

**Never:**
- replace, re-sign, restart or quit the installed Mewla.app or its daemon, or touch its state, Keychain items or TCC
  rows;
- run `tccutil reset`, write TCC.db, drive System Settings, use sudo, or `launchctl` anything that is not the Worker's
  own.

Before any capture or element action, the Worker checks that the installed Mewla's grants it works through are there
(`CGPreflightScreenCaptureAccess`, `AXIsProcessTrusted`). If they are missing, it stops and tells Brain rather than
prompt. At the end it stops Mewla Dev and deletes its state.
