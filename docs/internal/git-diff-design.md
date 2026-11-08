# Git Review Design

Product and interaction specification for the Git review sheet opened from a
Session. It is a working tool, not a read-only file list: a person on a phone
sees what an agent changed, reads the diffs, and accepts them (stage, commit,
push), throws them away (discard), or sends the agent back (a comment on a hunk
or line). Data and protocol semantics are in `docs/internal/git-review.md`.

## References

What the sheet took from the tools people already know, and what it left out:

- **VS Code Source Control, Fork, Lazygit:** two groups, Staged and Changes, so
  stage and unstage are one tap on a row, and a file edited both ways appears in
  both groups. Kept. Their stage-by-line and conflict editors were left out.
- **Working Copy, GitHub mobile:** one-line rows, the diff one tap away, hunks as
  the unit of review, next/previous file without going back. Kept. A reviewing
  workflow with approvals is not part of it.
- **Pierre's own review UI:** Pierre Diffs draws every diff, with line selection;
  Pierre Trees stays the Files browser. The changed-files list is native, because
  a row there carries a stage toggle and two groups, which a WebView tree
  cannot host and two of them would cost two documents.
- **Mewla-specific:** "Ask the agent" turns a selected hunk or lines into a message
  to the Session's agent with `file:line` and the quoted lines.

## Information architecture

One screen in two levels, the same on Android, iOS and desktop web.

- **Top bar (48 pt):** Close, the text tabs `Changes | History`, then Search, the
  list/folder toggle, and the ⋮ menu (Refresh, Browse files, Fetch, Pull, Push,
  Undo last discard, Discard all changes, copy path and branch).
- **Status strip (40 pt):** the branch chip (opens the branch sheet), the one
  summary `+416 −10 · 14 files` (the only place it appears), and the next sync
  step as one button: `Publish`, `Push ↑2`, `Pull ↓1`; nothing when level.
- **Search** is a row that appears from the top bar's Search button and filters by
  every word of the path; it is never standing chrome.
- **Groups:** `Staged n` and `Changes n`, each collapsible, with its `+/−` and a
  bulk action (`Unstage all`, `Stage all`).
- **Row (40 pt):** status mark, file name, folder (cut from the end), `+a −d`, and
  the stage toggle (a full touch target). Tap opens the diff; long press opens
  View diff, Stage/Unstage, Discard, Open working file, Copy path. The status is a
  one-character mark (`•` modified, `+` added, `−` deleted, `→` renamed, `?`
  untracked, `!` conflict), not a tile.
- **Folder mode** nests the same rows and joins single-folder chains into one row.
  The choice is remembered for the app session.
- **Primary bar:** `Commit n files` when something is staged, otherwise
  `Stage all n`. Commit opens a message sheet with Commit and Commit & push.
- **History:** the branch's commits (subject, short sha, author, age, refs). A
  commit opens its files; each file opens its diff in the same reader.
- **Reader (phone: over the list; wide: right pane at 720 pt and up):** header
  with Back, name, `+a −d`, folder, previous/next file, ⋮ (find, display, open
  working file, copy path, copy diff, refresh). One bottom bar:
  `✕ ‹ 2/5 › | Ask | Discard | Stage hunk`. With no hunk selected the same
  controls act on the whole file (`Stage file`). Staged diffs offer `Unstage`;
  commit diffs offer only the hunk arrows.
- **Selecting a hunk:** the hunk arrows select the next or previous hunk (highlighted
  and scrolled to), or tap a line number to select a line and its hunk.
- After a change the reader moves on to the file now at the same place, so a
  review can be: open, stage hunk, stage file, continue.

## Actions and safety

- Stage, unstage, hunk actions, commit, push, pull, branch switch and create are
  one request each and show Git's own message when they fail (a rejected push, a
  diverged pull, a conflict, a switch blocked by local changes) in an error toast.
- Discard always asks first (`confirmDestructive`) and then offers Undo for 7
  seconds, and afterwards in ⋮ as `Undo last discard`. A file's content is saved
  as a Git object before it is thrown away; Undo refuses if the file changed since.
  Discard is offered only on the Changes group; a staged file is unstaged first.
- Discard uses danger ink, never the seal colour.
- **Ask the agent** appears only on a Session that runs an agent. It queues the
  message through the same outbox as the chat composer, so it is retried and
  appears in the chat.
- Opening the review runs one `git fetch` so ahead/behind is true.

## Accessibility

- Every icon control has a spoken label; row labels carry name, status, counts and
  staged state. Hunk arrows are also the screen reader's increment and decrement.
- Touch targets are the platform minimum around small glyphs.
- Back unwinds `file → list`, `commit file → commit → history`, and `search`; Android
  hardware back follows the same stack.
