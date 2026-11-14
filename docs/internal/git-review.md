# Git Review

Git review works on the current server and the Session's working directory: it
shows what changed there and can stage, unstage, discard, commit, push, pull,
switch branches and read history, all by running `git` on the daemon in that
repository. Android and iOS share every feature; desktop web uses the same code
with a two-pane layout. Interaction layout is specified in
`docs/internal/git-diff-design.md`. The notes below cover data, actions and
freshness.

## Groups

The status groups changes the way the index holds them:

- **Staged**: HEAD against the index, including an initial, unborn index.
- **Changes**: the index against the working tree, plus untracked files as
  additions. A new folder is one row (`app/`, `directory`), as `git status`
  shows it, not one row per file inside: it can be staged, has no diff of its
  own and is not discarded here.

A file edited both ways is listed once in each group, and each row opens that
group's patch (`gitDiffSectionsForGroup`). Git handles rename detection, binary
classification, submodules and diff generation. Paths can be filtered by
destination or rename source. No arbitrary commit/branch comparison is implied.

## Reader

The client draws the review natively (`client/ui/more/git`: `GitDiff.tsx`,
`GitFiles.tsx`) from the models in `client/core/git`, using NSH's `DiffView` and
`Tree` components (`nsh/base`). The changed-file list has two groups, and each
row carries a stage toggle.

One file is open at a time. A layout 720 pt wide or more (`SPLIT_WIDTH`) reads in
split view, narrower in unified view. Display options toggle line wrap (on by
default), the code text size and file headers. Unchanged context expands in
place when the file has staged or unstaged edits but not both and neither side
is binary or truncated (see Data And Freshness).

Find is over the whole open diff (`findGitPatchMatches`); its arrows step
through the matching lines and the count is announced. The hunk actions select a
hunk by line (`gitHunkAtLine`, `gitHunkSelection`).

The Files browser is a `Tree` over every tracked and untracked-but-not-ignored
path of the working tree, with a file opening into the reader's file view (its
byte limit is separate: a truncated file says so).

## Actions

Mutations are `git_stage`, `git_unstage`, `git_discard`, `git_discard_undo`,
`git_hunk`, `git_commit`, `git_push`, `git_pull`, `git_fetch` and `git_switch`.
Each answers `{type: "git_action", request_id, action, result}`, or an error frame
with Git's own message (trimmed, without hints, ANSI or URLs). Reads are
`git_branches`, `git_log` and `git_show`.

- Every request names the Session (`target_id`/`cwd`) and the repository the person
  is looking at (`repo_root`); the daemon refuses when the terminal has since moved
  to another repository. Git runs with argument arrays (no shell), pathspecs as
  `:(literal)` and validated paths (no absolute, `..` or `.git`); a branch name goes
  through `check-ref-format --branch`, a sha through `rev-parse --verify`.
- Mutations of one repository run one at a time; if Git reports a lock, the message
  says another git command is running. `git_fetch` only moves remote-tracking refs
  and does not wait for them. Commands carry a timeout (30 s local, 60 s for the
  network), `GIT_TERMINAL_PROMPT=0` and `ssh -o BatchMode=yes`, so an unreachable
  remote or a credential prompt fails instead of hanging.
- `git_hunk` takes only the hunk text (from `@@`); the daemon builds the file header
  from the current diff itself, validates the hunk's line counts, and applies it with
  `git apply` (`--cached`, `--reverse` or both as the action needs). A hunk that no
  longer applies says so. A discarded hunk is undone by applying it forward.
- `git_discard` saves each affected file with `git hash-object -w` and returns
  `undo` tokens (`path`, `blob`, `after`, `mode`, `staged`); `git_discard_undo`
  restores them and refuses when the file no longer matches `after`.
- `git_commit` takes the message on stdin (`-F -`); `git_push` publishes with
  `-u origin <branch>` when there is no upstream; `git_pull` is `--ff-only`.
- The status adds `upstream`, `ahead`, `behind`, `detached`, `has_commits` and
  `operation` (a merge, rebase, cherry-pick or revert in progress).
- "Ask the agent" sends no request of its own: the app queues a message to the
  Session through the chat outbox, with `file:line` and the quoted lines.

## Data And Freshness

Update both the client and its current daemon when installing Git review:
`Unknown message type: git_repo_paths` means the connected daemon predates the
Files tree. Verify the current server identity and deploy the matching daemon.

`git_diff_status` uses NUL-delimited status and batched statistics, preserving
literal filenames, including whitespace and Git pathspec metacharacters. It
lists at most 1000 files (tracked changes before untracked ones); the counts and
`+/−` totals cover them all and `truncated` says some were left out. With
`repository_only` it answers only whether the folder is a repository (no status,
no files): a Session's header shows its Git button by that. The page draws 200
rows per group and offers the rest 200 at a time.
`git_diff_patch` returns the complete patch of one file as sections (`staged`,
`unstaged`, `untracked`); the client keeps the sections of the selected
group (Changes is unstaged plus untracked) and hands them to the reader.
`git_diff_file_content` returns the full HEAD and working-tree text, which is
what hunk expansion uses. Expansion is offered only when those two sides are
exactly what the patch compares, that is when a file has staged or unstaged edits
but not both, and neither side is binary or truncated; otherwise the hunks stay
as git printed them. `git_repo_paths` returns up to 100000 repo-relative paths
(`truncated` says when it was capped).

A comparison larger than 5 MB (`GIT_DIFF_PATCH_LIMIT_BYTES`) is not shown; the sheet
says it is too large, and Copy diff still copies it. There is no daemon-side
paging: the patch is parsed in one pass.

A reader is a snapshot. Refresh reloads the overview and selected diff, and every
change the sheet makes reloads them itself. Expansion
reads the working tree when it is asked for, so a file edited after its patch was
loaded can expand to lines that no longer match until Refresh. Repository errors
are visible even after an earlier successful overview. Changing server, session,
working directory, or connection clears the previous owner's data. Late
responses cannot restore it.

## Verification

Disposable Git fixtures (a repository, a bare remote and a second clone, with
Git's global configuration disabled) and authenticated WebSocket regressions are in
`daemon/server/git_diff_*_test.go` and `git_actions*_test.go`; they cover conflict,
diverged, stale-hunk and rejected-push cases. Run:

```sh
cd daemon
go test ./server -run 'Git'
go test ./server -run '^$' -bench '^BenchmarkGitDiff$' -benchtime=3x
```

The benchmark includes small, medium, many-file, large-single-file, and long-line
repositories. `TestGitDiffNativeFixtureServer` is opt-in with
`MEWLA_GIT_DIFF_NATIVE=1`; it binds only loopback port 8097 and uses the production
authenticated WebSocket handler against temporary repositories. `/stop` shuts
down this test resource. It is not a daemon entry point or a product route.

The app side is `cd client && bun test core/git` and `cd client/ui && nsh test more/git`.
The review's pure models (groups, rows, hunks, the agent message) are in
`client/core/git/gitReview.test.ts`.

Native performance evidence must come from Android/iOS execution, not these Go
or Bun tests. Android emulator data, debug-build overhead, and software-rendered
frame timings must be distinguished from release-device performance. iOS
Simulator verification requires macOS.
