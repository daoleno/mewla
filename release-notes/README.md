# What's New

One file per release, `<version>.json`: what the app shows once after Mewla changed to that version (owner and
Brain, 2026-11-16). A gift, not a changelog: the two or three things a person will feel, each with something to try.

Every release has its file (owner, 2026-11-20): otherwise a person on the new version is shown the old version's
notes. Whoever lands a feature a person will feel adds it to the next version's file in the same commit, and a fix
the person will notice to its `fixes`. The release preparation is the backstop: `scripts/prepare-release.py` refuses a
version without its file, and `scripts/verify-release-identity.sh` (CI, and the release at its tag) refuses a tracked
version without one, so a manual tag cannot ship one either.

```json
{
  "version": "0.4.9",
  "date": "2026-11-16",
  "highlights": [
    {
      "id": "0.4.9-back-after-network-switch",
      "title": "Back in seconds after switching networks",
      "line": "Wi-Fi to mobile data and back: Mewla reconnects as soon as the network does.",
      "media": {"scene": "network-hop"},
      "try": {"kind": "open", "target": "computer"}
    }
  ],
  "fixes": ["A Brain thread's chat opens on its own messages."]
}
```

- **highlights**: at most four; the sheet shows at most three, newest release first, each missed release's first
  highlight before any second one. Empty for a release of fixes only: no sheet, only the line in Brain's chat
  ("Updated to 0.4.9 · What's new"), which lists `fixes`. A release with something a person will feel has a
  highlight: 0.4.19 filed share to Mewla, the amber Git button and Work that closes itself as nine paragraphs of
  fixes, and people got a wall of text instead of a sheet (owner, 2026-11-22).
- **fixes**: from 0.4.20 on, at most three, each at most 100 characters: the list a release of fixes only shows,
  read at a glance. More than that is a changelog; give what a person will feel a highlight with its scene and
  leave out what they will not notice. `scripts/release-notes.py` refuses more (and so do the release preparation
  and the release identity check); files before 0.4.20 shipped as they are.
- **id**: `<version>-<name>`, used once ever. The computer keeps the ids the person has seen.
- **title**: at most six words, no version (the sheet shows it apart). **line**: one line, at most 90 characters,
  no ellipsis. English, in the person's words: what they can now do, never how it was built. A fix the person felt
  or asked for can be a highlight ("Fixed: …").
- **media.scene**: the loop drawn in `client/ui/whatsnew/scenes.tsx` (`chat-card`, `update-restart`,
  `agent-windows`, `browser-survives`, `network-hop`, `update-line`, `chat-diagram`, `chat-copy`, `screen-reflow`, `screen-pointer`, `picture-save`,
  `work-menu`, `trigger-watch`, `share-in`, `attach-sheet`, `git-button`, `work-closes`, `mac-app`, `mac-window`,
  `image-drop`, `page-zoom`, `long-chat`, `tool-ledger`, `any-provider`, `provider-presets`, `provider-usage`, `wide-beside`, `preview-beside`). A new feature usually gets a new scene there;
  `WhatsNew.test.tsx` fails on a name the app does not draw. To look at them: `client/scripts/shots.sh --entry
  ui/whatsnew/testing/ScenesShots.tsx scripts/shots/whatsnew-scenes.js` (frames of each loop), and
  `--entry ui/whatsnew/testing/WhatsNewShots.tsx` with `scripts/shots/whatsnew.js` (the sheet) or
  `whatsnew-fixes.js` (a fixes list).
- **try**: `{"kind": "open", "target": …}` with brain, sessions, new-session, screen, calendar, computer or
  settings; or `{"kind": "prompt", "target": "<text>"}`, written into Brain's composer.
- **platforms** (optional): for a feature only some platforms have, the ones that do, by the app's `platform.os`
  (`android`, `ios`, `web`, `macos`), e.g. `["android"]` for sharing to Mewla. A person elsewhere is not offered it,
  and their sheet does not mark it seen, so their Android phone still shows it. Leave it out when every platform
  has the feature.

After editing, run `python3 scripts/release-notes.py`: it checks every file and writes the copies the app and the
daemon ship. The daemon's copy has every release's line and fixes but no highlights: an app older than its computer
cannot draw a newer release's scenes or honour its `platforms`, and the sheet it showed would mark them seen, so the
person never saw them drawn. Such an app shows the newer release's line; its sheet comes with the app's own update. `--check` (CI) fails on a stale copy; `--require <version>` (release preparation, the release identity
check) on a missing file.

To see the sheet again on a computer: `mewla dev whats-new`. The next quiet Brain home, on the phone or the web,
shows it as after an update from before any of these releases.
