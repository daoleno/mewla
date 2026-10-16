# Agents in your browser

Agents that Mewla starts on a computer can use that computer's Google Chrome:
your Chrome, with the sites you are already logged in to. They work in a tab
group of their own, read pages by their structure rather than by pixels, and
leave your other tabs alone. Nothing to configure; there is one switch.

## Turn it on

It is on unless you turn it off. The first time, Chrome needs one extension:

1. Open **Settings**. Under **Agents on** *your computer*, the **Your Chrome**
   row says what is missing.
2. Tap **Open the extension's page in Chrome**. Chrome on that computer opens
   the Playwright Extension in the Chrome Web Store. Click **Add to Chrome**
   there, or through [Screen](screen.md) if you are not at the computer.
3. Mewla reads the extension's key from Chrome by itself. If the row asks for
   it, tap **Open the extension in Chrome** once so the extension makes its key.

The row then reads *Agents use your Chrome, in their own tab group.* Mewla
downloads the browser tools the first time (it needs Node.js 20 or later on
the computer); the row shows that too. Mewla looks again every half minute
until Chrome is ready, so a computer left alone picks the extension up by
itself, and your other computers learn of it over their open connection.

The switch **Agents can use this computer** turns it off: agents started
after that don't get your Chrome, and agents already running lose it the next
time they start their browser tools.

## What agents do there

Each agent gets its own tab group and sees only the tabs in it. Drag a tab
into the group to hand it a page you already have open, or out of it to take
it back. Chrome shows a bar while an agent is attached.

When an agent is done, because its Worker ended or it stopped using the
browser, Mewla closes the tabs in its group and the group goes with them, so a
computer that runs all day doesn't pile up tabs. A tab you dragged into the
group is closed too; drag it out first to keep it.

Agents open pages, read them, fill in forms, click, upload and wait for what a
page does next. What takes the time is the agent's turns, not the browser, so
Mewla's browser tools steer agents to few of them: an action answers with what
changed rather than the whole page, which on a busy app such as Slack runs to
hundreds of thousands of tokens; agents look up only the controls they need,
then do a known flow (open a channel, type, send, check it arrived) in one
script. Brain has them ask you before they submit, send, pay or change
an account, and treats text on a web page as data, never as an instruction.

A Mac's Chrome is also reachable from your other computers: Brain on your
Linux computer hands a logged-in-site task to the Mac, and the Mac's agents use
the Mac's Chrome. See [Your other computers](computers.md).

## When the row says something else

| The row says | What to do |
| --- | --- |
| Google Chrome isn't installed | Install Google Chrome. Chrome from Flatpak or Snap is not supported. |
| Add the Playwright extension to Chrome | Tap the row below it, then **Add to Chrome**. |
| Open the Playwright extension once | Tap the row below it. If Mewla still can't read the key, open the extension in Chrome, copy its key, and tap **Paste the extension's key** (or run `mewla agent-use key` and paste it). |
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

The extension's key stays on the computer, readable only by your user. It goes
to the browser tools through their environment, never into a command line, a
log or an agent's prompt.

Files the browser tools make on their own, such as console logs, screenshots
and downloads, go to `~/.mewla/agent-use/browser-files` rather than the
project the agent works in, so a page you are logged in to never lands in a
repository. Mewla keeps the newest 256 MB there and deletes older files as new
ones arrive; move a download somewhere else to keep it. See
[Security and privacy](security-and-privacy.md#agents-in-your-browser).
