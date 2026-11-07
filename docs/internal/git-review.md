# Git Review

Git Diff in a terminal reviews changes on that terminal's current server and
working directory. Android and iOS share the same overview, comparisons, reader,
search, and navigation. Reviewing does not stage, discard, or otherwise modify
the repository.

Interaction layout, navigation, scrolling, safe-area and acceptance criteria are
specified in `docs/internal/git-diff-design.md`. The notes below cover data and freshness
semantics.

## Comparisons

- **All** lists staged, working-tree, and untracked changes. A file changed in both
  index and working tree has separate, explicitly labeled patch sections.
- **Working** compares the index with the working tree and includes untracked
  files as additions.
- **Staged** compares HEAD with the index, including an initial, unborn index.

Overview counts follow the selected comparison. The top-level repository summary
includes all changes. Paths can be filtered by destination or rename source.
Git handles rename detection, binary classification of patches, submodules, and
diff generation. No arbitrary commit/branch comparison is implied by these modes.

## Reader

The reader is [@pierre/diffs](https://diffs.com) (the file view too) and the
changed-file list and Files browser are [@pierre/trees](https://trees.software),
both running from bundled offline copies (see
`docs/third-party-assets.md`). On Android and iOS each is a WebView document that
cannot navigate or fetch; on web the same code mounts straight into the page,
because the web UI's CSP allows only same-origin scripts. The sheet keeps all
chrome in React Native (header, scope control, find bar, display options, action
menu); the WebViews draw content only. Messages between them are typed in
`app/components/terminal/pierre/diffViewerProtocol.ts` and
`treeViewerProtocol.ts`, and every message from a WebView is parsed and dropped
if unexpected.

One file is open at a time. A wide layout (720 dp and up) reads in split view and
a phone in unified view. The reader shows old and new line numbers, change
colors, word-level changes inside a line, syntax highlighting, and collapsed
unchanged context that expands in place. Syntax comes from a fixed set of Shiki
grammars; any other extension reads as plain text. Colors come from the terminal
palette and the font is the terminal's, so a diff matches the terminal beside it.

Back unwinds one level at a time (`file -> tree -> changes -> close`); Android
hardware back follows the same stack. The reader WebView is one long-lived
document per sheet: it stays mounted and empty while the list shows, so opening a
file never pays the runtime's start-up, and it restores each comparison's scroll
position. Wrap, text size and file headers re-render the open comparison in
place. A file with both staged and unstaged edits shows one labeled section per
half.

Find is case-insensitive over the whole selected comparison, runs inside the
reader, wraps around at either end and highlights the current line. Its arrows
step through matching lines; screen-reader actions step through hunks and stop at
the first and last.

The changed-file list is a tree with git status marks, `+a -d` counts for the
selected comparison and Trees' own path search (shown from six files up). A tap
opens the diff in one step; a long press opens the file's action menu. The Files
browser is the same tree over every tracked and untracked-but-not-ignored path
of the working tree, not a historical comparison, with a file opening into the
reader's file view (its byte limit is separate: a truncated file says so).

## Data And Freshness

Update both the client and its current daemon when installing Git review:
`Unknown message type: git_repo_paths` means the connected daemon predates the
Files tree. Verify the current server identity and deploy the matching daemon.

`git_diff_status` uses NUL-delimited status and batched statistics, preserving
literal filenames, including whitespace and Git pathspec metacharacters.
`git_diff_patch` returns the complete patch of one file as sections (`staged`,
`unstaged`, `untracked`); the client keeps the sections of the selected
comparison (Working is unstaged plus untracked) and hands them to the reader.
`git_diff_file_content` returns the full HEAD and working-tree text, which is
what hunk expansion uses. Expansion is offered only when those two sides are
exactly what the patch compares, that is when a file has staged or unstaged edits
but not both, and neither side is binary or truncated; otherwise the hunks stay
as git printed them. `git_repo_paths` returns up to 100000 repo-relative paths
(`truncated` says when it was capped).

A comparison larger than 5 MB is not sent to the reader; the sheet says it is too
large, and Copy diff still copies it. There is no daemon-side paging: the patch
crosses the WebView bridge once and is parsed in one pass. (`git_diff_page` and
`git_repo_entries` remain in the daemon but nothing in the app calls them.)

A reader is a snapshot. Refresh reloads the overview and selected diff. Expansion
reads the working tree when it is asked for, so a file edited after its patch was
loaded can expand to lines that no longer match until Refresh. Repository errors
are visible even after an earlier successful overview. Changing server, session,
working directory, or connection clears the previous owner's data. Late
responses cannot restore it.

## Verification

Disposable Git fixtures and authenticated WebSocket regressions are in
`daemon/server/git_diff_*_test.go`. Run:

```sh
cd daemon
go test ./server -run 'GitDiff|GitRepo'
go test ./server -run '^$' -bench '^BenchmarkGitDiff$' -benchtime=3x
```

The benchmark includes small, medium, many-file, large-single-file, and long-line
repositories. `TestGitDiffNativeFixtureServer` is opt-in with
`MEWLA_GIT_DIFF_NATIVE=1`; it binds only loopback port 8097 and uses the production
authenticated WebSocket handler against temporary repositories. `/stop` shuts
down this test resource. It is not a daemon entry point or a product route.

The app side is `bun test components/terminal`. The generated viewers are
rebuilt with `bun run pierre:generate` (`--check` reports stale output).

Native performance evidence must come from Android/iOS execution, not these Go
or Bun tests. Android emulator data, debug-build overhead, and software-rendered
frame timings must be distinguished from release-device performance. iOS bundles
can be exported on Linux, but iOS Simulator verification requires macOS.
