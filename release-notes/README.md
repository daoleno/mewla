# What's New

One file per release, `<version>.json`: what the app shows once after Mewla changed to that version (owner and
Brain, 2026-11-16). A gift, not a changelog: the two or three things a person will feel, each with something to try.

```json
{
  "version": "0.4.9",
  "date": "2026-11-16",
  "highlights": [
    {
      "id": "0.4.9-follows-the-network",
      "title": "Stays connected as you move",
      "line": "From Wi-Fi to mobile data and back, Mewla follows without dropping.",
      "media": {"scene": "network-hop"},
      "try": {"kind": "open", "target": "computer"}
    }
  ],
  "fixes": ["A Brain thread's chat opens on its own messages."]
}
```

- **highlights**: at most four; the sheet shows at most three, newest release first, each missed release's first
  highlight before any second one. Empty for a release of fixes only: no sheet, only the line in Brain's chat
  ("Updated to 0.4.9 · What's new"), which lists `fixes`.
- **id**: `<version>-<name>`, used once ever. The computer keeps the ids the person has seen.
- **title**: at most six words, no version (the sheet shows it apart). **line**: one line, at most 90 characters,
  no ellipsis. English, in the person's words: what they can now do, never how it was built. A fix the person felt
  or asked for can be a highlight ("Fixed: …").
- **media.scene**: the loop drawn in `client/ui/whatsnew/scenes.tsx` (`chat-card`, `update-restart`,
  `agent-windows`, `browser-survives`, `network-hop`, `update-line`). A new feature usually gets a new scene there;
  `WhatsNew.test.tsx` fails on a name the app does not draw.
- **try**: `{"kind": "open", "target": …}` with brain, sessions, new-session, screen, calendar, computer or
  settings; or `{"kind": "prompt", "target": "<text>"}`, written into Brain's composer.

After editing, run `python3 scripts/release-notes.py`: it checks every file and writes the copies the app and the
daemon ship. `--check` (CI) fails on a stale copy; `--require <version>` (release preparation) on a missing file.

To see the sheet again on a computer: `mewla dev whats-new`. The next quiet Brain home, on the phone or the web,
shows it as after an update from before any of these releases.
