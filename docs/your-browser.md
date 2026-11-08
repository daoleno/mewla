# Agents in your browser

Every agent Mewla starts has one browser tool, `mewla_chrome`, on every
computer; the tool and its behaviour are the same everywhere. Only where the
agent's Chrome window lives differs. On a Mac, or a desktop without the virtual
screen, it is your own Chrome (this page). On Linux with **Settings → Labs →
Virtual screen** on, it is the agents' own Chrome on that virtual screen, each
agent's window on a monitor of its own, with the same Mewla extension, which
Mewla loads itself: there is nothing to add and no sign-in of yours in it (see [Screen](screen.md#linux-agents-on-a-virtual-screen)).

Agents that Mewla starts on a computer can use that computer's Google Chrome:
your Chrome, with the sites you are already logged in to. They work in a tab
group of their own, read pages by their structure rather than by pixels, and
leave your other tabs alone. Nothing to configure; there is one switch.

## Turn it on

It is on unless you turn it off. The first time, Chrome needs one extension:

1. Open **Settings** and tap the computer. Its page has a quiet row,
   *Let agents use Chrome*. A missing Chrome or Node.js is not shown there as a
   problem; `mewla agent-use` on the computer says what is missing.
2. Tap **Set up**. Chrome on that computer opens its extensions page, and Mewla
   says which folder to load. Turn on **Developer mode**, choose **Load
   unpacked** and pick `~/.mewla/extension/mewla` (the folder Mewla wrote).
   Do this at the computer or through [Screen](screen.md). Once the extension
   is in the Chrome Web Store, **Set up** opens its page there and you click
   **Add to Chrome** instead.

That is all: no key to copy. Mewla registers its helper with Chrome itself, and
the extension connects to it whenever Chrome runs. If Chrome isn't running when
an agent needs it, Mewla starts it in the background with no window, and the
agent's window is the only one that appears.

Mewla downloads the browser tools the first time (it needs Node.js 20 or later
on the computer). A computer without a desktop session, such as a headless
server, has no Chrome for agents to use; its page shows nothing about it. Mewla
looks again every half minute until Chrome is ready, so a computer left alone
picks the extension up by itself, and your other computers learn of it over
their open connection.

If you set the Playwright extension up before, with its key, that keeps
working; adding the Mewla extension replaces it.

The switch **Agents can use this computer** turns it off: agents started
after that don't get your Chrome, and agents already running lose it the next
time they start their browser tools.

## What agents do there

Each agent gets a window of its own and sees only the tabs in it, in a tab
group named for the Worker, so you can see whose tabs they are. Several agents
work in your Chrome at the same time, each in its own window, none waiting for
another; on an agent display each window is on its agent's monitor. Your own
windows and tabs are never read, moved or focused, and Chrome stays behind
whatever you are using. On a Mac with an agent display (see [Screen](screen.md))
the window opens there, off your screen. Where there is no agent display, the
window opens minimized and stays out of the way, its tabs opened in the
background; if a desktop still gives it the focus for a moment (opening a
window, a click in a page), Mewla gives the focus straight back to the window you
were in. Chrome shows a bar while an agent is attached.

When an agent is done, because its Worker ended or it stopped using the
browser, Mewla closes its window and the tabs in it, so a computer that runs
all day doesn't pile up tabs.

Agents open pages, read them (a page's text too, with `browser_read`), fill in
forms, click, upload and wait for what a page does next. A page that never
finishes loading does not hold them: a navigation answers within twelve seconds,
and they read what is there. What takes the time is the agent's turns, not the browser, so
Mewla's browser tools steer agents to few of them: an action answers with what
changed rather than the whole page, which on a busy app such as Slack runs to
hundreds of thousands of tokens; agents take the controls they need from your
request and do the whole flow (fill a multi-step form, open a channel, type a
message, read it back) in one script, looking at the page only if the script
misses. Each action also waits only a tenth of a second for the page to settle
instead of Playwright's half second. A script sends, submits or pays only what you already approved;
otherwise it stops before that step and the agent asks. Brain has them ask you before they submit, send, pay or change
an account, and treats text on a web page as data, never as an instruction.

A Mac's Chrome is also reachable from your other computers: Brain on your
Linux computer hands a logged-in-site task to the Mac, and the Mac's agents use
the Mac's Chrome. See [Your other computers](computers.md).

## When the row says something else

| The row says | What to do |
| --- | --- |
| Google Chrome isn't installed | Install Google Chrome. Chrome from Flatpak or Snap is not supported. |
| Add the Mewla extension to Chrome | Tap **Set up**, then load the Mewla folder (Developer mode, **Load unpacked**), or **Add to Chrome** once it is in the store. |
| Open the Playwright extension in Chrome and copy its key | Only for the older Playwright extension: tap **Open it**, copy the key it shows, then run `mewla agent-use key` on the computer and paste it. Adding the Mewla extension instead needs no key. |
| Sign in to this computer's desktop | On Linux, agents open Chrome in your desktop session. With nobody signed in to the desktop there is no Chrome to use; sign in and the row turns ready. |
| Install Node.js 20 or later | Install Node.js on that computer; Mewla finds it in the usual places, including version managers. |
| Couldn't download the browser tools | Check the computer's network, then tap **Try again**. |

From a terminal on the computer:

```sh
mewla agent-use status   # what agents can use, and the one fix if not
mewla agent-use off      # the same switch as in the app
mewla agent-use fix      # do the fix that status names
```

## What stays where

The Mewla extension has no key. (With the older Playwright extension, its key stays
on the computer, readable only by your user, and goes to the browser tools
through their environment, never into a command line, a log or an agent's
prompt.)

Files the browser tools make on their own, such as console logs, screenshots
and downloads, go to `~/.mewla/agent-use/browser-files` rather than the
project the agent works in, so a page you are logged in to never lands in a
repository. Mewla keeps the newest 256 MB there and deletes older files as new
ones arrive; move a download somewhere else to keep it. See
[Security and privacy](security-and-privacy.md#agents-in-your-browser).
