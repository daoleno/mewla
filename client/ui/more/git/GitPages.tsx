import { effect, Scroll, signal, Text, untracked, View, type ViewRef } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { NavPage } from "../../shell/NavPage";
import type { GitCommitSummary } from "../../../core/git/gitDiff";
import { gitDiffStatusWord, gitDiffTone } from "../../../core/git/gitDiffPresentation";
import {
  buildGitReviewRows,
  gitReviewSummary,
  gitReviewSync,
  gitSyncAction,
  splitGitReviewPath,
  sumGitReviewEntries,
  type GitReviewEntry,
  type GitReviewGroup,
  type GitReviewRow,
} from "../../../core/git/gitReviewModel";
import { formatGitAge, gitCommitDays, spokenGitAge } from "../../../core/git/gitReviewTime";
import { DisplayTitle, EmptyState, Notice, Page, PillButton, Row, RowButton, SearchField, Section, SectionLabel, Spinner, Tag } from "../../kit/kit";
import { Icon } from "../../kit/Icon";
import { Segmented } from "../../kit/Segmented";
import type { PageNav } from "../routes";
import { CommitFilePage, DiffPage, DiffReader } from "./GitDiff";
import { listDetail } from "../../shell/layout";
import { createMenuAnchor, showMenu, type MenuItem } from "../../kit/Menu";
import { FilePage, FilesPage } from "./GitFiles";
import { Counts, Current, FileName, FlashLine, IconButton, StatusTile, TextButton } from "./parts";
import type { GitPage } from "./routes";
import { BranchSheet, CommitSheet } from "./sheets";
import { copyText, gitReview, say, type GitReview } from "./state";
import { showAlert } from "../../shell/alert";

/** The page for a Git route, on the computer that was current when it opened. */
export function GitPageView(props: { route: GitPage; nav: PageNav }) {
  const route = props.route;
  const review = gitReview({ targetId: route.targetId, cwd: route.cwd });
  if (!review) return <Page title="Git"><EmptyState icon="server" title="No current server" detail="Choose one in Settings." /></Page>;
  switch (route.name) {
    case "git": return <ReviewPage review={review} title={route.title} nav={props.nav} />;
    case "git-diff": return <DiffPage review={review} entry={route.entry} nav={props.nav} />;
    case "git-commit": return <CommitPage review={review} sha={route.sha} nav={props.nav} />;
    case "git-commit-file": return <CommitFilePage review={review} sha={route.sha} path={route.path} nav={props.nav} />;
    case "git-files": return <FilesPage review={review} nav={props.nav} />;
    case "git-file": return <FilePage review={review} path={route.path} nav={props.nav} />;
  }
}

function target(review: GitReview) {
  return { targetId: review.target.targetId, cwd: review.target.cwd };
}

export function openWorkingFile(review: GitReview, nav: PageNav, path: string): void {
  nav.push({ name: "git-file", ...target(review), path });
}

/** The row menu: what a long press on a changed file offers. */
async function entryMenu(review: GitReview, nav: PageNav, entry: GitReviewEntry, open: (entry: GitReviewEntry) => void): Promise<void> {
  const staged = entry.group === "staged";
  const answer = await showAlert({
    title: entry.file.path,
    presentation: "actionSheet",
    actions: [
      { id: "view", label: "View diff" },
      { id: "stage", label: staged ? "Unstage" : "Stage" },
      ...(staged ? [] : [{ id: "discard", label: "Discard changes" }]),
      { id: "open", label: "Open working file" },
      { id: "copy", label: "Copy path" },
      { id: "cancel", label: "Cancel", role: "cancel" as const },
    ],
  });
  if (answer.action === "view") open(entry);
  else if (answer.action === "stage") void review.toggleStaged(entry);
  else if (answer.action === "discard") void review.discardFile(entry);
  else if (answer.action === "open") {
    // A deleted file has no working copy to open: say so, as the old menu's disabled item did.
    if (entry.file.status === "deleted") say({ title: "Deleted from the working tree", detail: entry.file.path, tone: "neutral" });
    else openWorkingFile(review, nav, entry.file.path);
  } else if (answer.action === "copy") void copyText(entry.file.path, "Path copied", entry.file.path);
}

/** More actions on the whole repository. */
async function overflowMenu(review: GitReview, nav: PageNav, anchor: ViewRef | null): Promise<void> {
  const snapshot = review.status();
  // SPEC §15 Code review's order: Refresh, Browse files, Fetch, Pull, Push, Copy path, Copy branch, Undo last
  // discard (disabled until there is one; a phone's action sheet leaves it out), then Discard all changes last, in ink.
  const actions: MenuItem[] = [{ id: "refresh", label: "Refresh" }];
  if (snapshot?.available) {
    actions.push({ id: "browse", label: "Browse files" });
    if (snapshot.upstream) actions.push({ id: "fetch", label: "Fetch" }, { id: "pull", label: "Pull" }, { id: "push", label: "Push" });
    if (snapshot.repo_root) actions.push({ id: "copy-root", label: "Copy path" });
    if (snapshot.branch) actions.push({ id: "copy-branch", label: "Copy branch" });
    actions.push({ id: "undo", label: "Undo last discard", disabled: !review.lastDiscard() });
    if (review.groups().changes.length) actions.push({ id: "discard-all", label: "Discard all changes" });
  }
  const title = [snapshot?.repo_name, snapshot?.branch].filter(Boolean).join(" · ");
  const chosen = await showMenu({ title: title || undefined, items: actions, anchor, testID: "git-menu-popover" });
  switch (chosen) {
    case "refresh":
      // Already reading: the answer on its way is the refresh.
      if (review.loading()) break;
      void review.refresh();
      review.reloadHistory();
      break;
    case "browse":
      nav.push({ name: "git-files", ...target(review) });
      break;
    case "fetch":
    case "pull":
    case "push":
      void review.sync(chosen);
      break;
    case "undo":
      review.undoLastDiscard();
      break;
    case "discard-all":
      void review.discardAll();
      break;
    case "copy-root":
      void copyText(snapshot!.repo_root!, "Path copied", snapshot!.repo_root!);
      break;
    case "copy-branch":
      void copyText(snapshot!.branch!, "Branch copied", snapshot!.branch!);
      break;
  }
}

type Tab = "changes" | "history";

/**
 * Git review: what changed (staged and not), the history, the branch and how it syncs.
 * On a wide screen the list and the reader sit side by side; on a phone a file opens
 * its own reader page. Back closes an open filter before it leaves.
 */
function ReviewPage(props: { review: GitReview; title?: string; nav: PageNav }) {
  const review = props.review;
  const tab = signal<Tab>("changes");
  const commitOpen = signal(false);
  const branchesOpen = signal(false);
  // The reader beside the list on a wide screen: the entry chosen, and the one it shows.
  // A fresh object per tap: tapping a row the reader moved away from opens it again.
  const chosen = signal<{ key: string } | null>(null);
  const moreAnchor = createMenuAnchor();
  const shown = signal<string | null>(null);
  // List and detail side by side on wide with a right pane of 880 or more (SPEC §14.2), following the pane live.
  const wide = () => listDetail();
  review.open();
  // History loads the first time its tab is shown.
  effect(() => {
    if (tab() === "history") untracked(() => !review.history().loaded && !review.history().loading && void review.loadHistory(0));
  });
  const snapshot = () => review.status();
  const title = () => snapshot()?.repo_name || props.title || "Git";
  // The project is the eyebrow over "Changes" or "History"; the nav bar names the project once that has scrolled away.
  const titleAway = signal(false);
  const closeSearch = () => {
    review.searchOpen.set(false);
    review.query.set("");
  };
  const open = (entry: GitReviewEntry) => {
    if (wide()) {
      if (shown() !== entry.key || !chosen()) chosen.set({ key: entry.key });
    }
    else props.nav.push({ name: "git-diff", ...target(review), entry: entry.key });
  };
  const headerRight = (
    <View class="flex-row items-center">
      <IconButton testID="git-search" icon="search" label="Search changed files" selected={review.searchOpen()} onPress={() => (review.searchOpen() ? closeSearch() : review.searchOpen.set(true))} />
      <IconButton testID="git-tree" icon={review.tree() ? "list" : "folder"} label={review.tree() ? "Show as list" : "Show as folders"} onPress={() => review.setTree(!review.tree())} />
      <IconButton ref={moreAnchor.ref} testID="git-more" icon="more-vertical" label="More actions" onPress={() => void overflowMenu(review, props.nav, moreAnchor.node())} />
    </View>
  );
  return (
    <NavPage
      title={titleAway() ? title() : ""}
      testID="git-page"
      column={wide() ? "full" : "page"}
      headerRight={headerRight}
      popOnBack={!review.searchOpen()}
      onBackPress={() => {
        if (review.searchOpen()) closeSearch();
      }}
    >
      <Current review={review} class="flex-1 bg-background">{() => (
        <View class="flex-1 flex-row bg-background">
          <View testID="git-list" class={wide() ? "w-[380px] border-r border-border" : "flex-1"}>
            <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="px-4 pb-6 pt-2" refreshing={review.loading() && !!snapshot()} onRefresh={() => { void review.refresh(); review.reloadHistory(); }}
              onScroll={(e) => titleAway.set(e.offset.y > 56)} scrollEventThrottle={64}>
              <View class="w-full max-w-[720px] self-center">
                <DisplayTitle eyebrow={title()} title={tab() === "changes" ? "Changes" : "History"} />
                <FlashLine />
                <View class="mt-3">
                  <Segmented testID="git-tab" value={tab()} onChange={(next) => tab.set(next)} options={[{ key: "changes", label: "Changes" }, { key: "history", label: "History" }]} />
                </View>
                <Strip review={review} tab={tab()} onBranches={() => branchesOpen.set(true)} />
                <If when={review.searchOpen() && tab() === "changes"}><View class="mt-3">
                    <SearchField testID="git-query" accessibilityLabel="Filter files" placeholder="Filter files" autoFocus value={review.query()} onChange={(value) => review.query.set(value)} />
                  </View></If>
                <View><If when={tab() === "changes"} else={<HistoryBody review={review} nav={props.nav} />}><ChangesBody review={review} nav={props.nav} selected={wide() ? shown() : null} onOpen={open} /></If></View>
              </View>
            </Scroll>
            <If when={tab() === "changes"}><CommitBar review={review} onCommit={() => commitOpen.set(true)} /></If>
          </View>
          <If when={wide()}><View testID="git-reader" class="flex-1 bg-background">
              <If when={chosen()} else={<View class="flex-1 px-4"><EmptyState testID="git-no-file" icon="document" title="No file selected" /></View>}>
                {/* Made again for each file opened from the list (a new choice); the reader's own steps keep it. */}
                <Each of={[chosen()!]} key={(choice) => choice}>{(choice) => <DiffReader
                  review={review}
                  entry={untracked(choice).key}
                  nav={props.nav}
                  onShow={(key) => shown.set(key)}
                  onEmpty={() => {
                    chosen.set(null);
                    shown.set(null);
                  }}
                />}</Each></If>
            </View></If>
          <CommitSheet review={review} open={commitOpen()} count={review.groups().staged.length} canPush={!!snapshot()?.available && !snapshot()?.detached} onClose={() => commitOpen.set(false)} />
          <BranchSheet review={review} open={branchesOpen()} onClose={() => branchesOpen.set(false)} />
        </View>
      )}</Current>
    </NavPage>
  );
}

/** Pinned under the list: Commit what is staged, else Stage all. */
function CommitBar(props: { review: GitReview; onCommit: () => void }) {
  const review = props.review;
  return (
    <View>
      {(() => {
        const snapshot = review.status();
        if (!snapshot?.available || snapshot.clean) return null;
        const groups = review.groups();
        if (!groups.staged.length && !groups.changes.length) return null;
        return (
          <View testID="git-commit-bar" class="bg-background px-4 pb-4 pt-1">
            <If when={groups.staged.length} else={<PillButton testID="git-stage-all" title={`Stage all ${groups.changes.length}`} busy={review.isBusy("bulk:changes")} onPress={() => void review.bulk("changes")} />}>{(length) => <PillButton testID="git-commit-open" filled title={`Commit ${length()} ${length() === 1 ? "file" : "files"}`} onPress={props.onCommit} />}</If>
          </View>
        );
      })()}
    </View>
  );
}

/** What the sync button says: "↑ 2 to push", "↓ 1 to pull", or Publish for a branch with no upstream. */
function syncLabel(sync: { kind: string; label: string }, snapshot: { ahead?: number; behind?: number } | null): string {
  if (sync.kind === "pull" && snapshot?.behind) return `↓ ${snapshot.behind} to pull`;
  if (sync.kind === "push" && snapshot?.ahead) return `↑ ${snapshot.ahead} to push`;
  return sync.label;
}

/** The branch as a pill (it opens Branches), how it syncs beside it, what changed in all, and a merge or rebase in progress. */
function Strip(props: { review: GitReview; tab: Tab; onBranches: () => void }) {
  const snapshot = () => props.review.status();
  return (
    <View>
      <If when={snapshot()?.available}><View class="mt-3 flex-row flex-wrap items-center gap-2">
          {(() => {
            const s = snapshot()!;
            const label = s.detached ? `Detached ${s.branch ?? ""}`.trim() : s.branch || "No branch";
            return (
              <Button testID="git-branch" accessibilityLabel={`Branch ${label}. Switch branch`} onPress={props.onBranches} class="h-9 max-w-[60%] flex-row items-center gap-1.5 rounded-full bg-card px-3.5 active:opacity-70">
                <Icon name="git-branch" size={16} tone="ink" />
                <Text class="min-w-0 shrink font-mono text-code font-semibold line-clamp-1">{label}</Text>
              </Button>
            );
          })()}
          {(() => {
            const sync = gitSyncAction(snapshot());
            return <If when={sync}>{(sync) => <TextButton testID="git-sync" label={syncLabel(sync(), snapshot())} busy={props.review.isBusy("sync")} onPress={() => void props.review.sync(sync().kind)} />}</If>;
          })()}
          <Text testID="git-summary" android={{ accessibilityLiveRegion: "polite" }} class="ml-auto font-mono text-code text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>
            {props.tab === "changes" ? gitReviewSummary(snapshot()) : gitReviewSync(snapshot())}
          </Text>
        </View></If>
      <If when={snapshot()?.operation}><View class="mt-2 px-1"><Text class="text-sub">{`${snapshot()!.operation} in progress`}</Text></View></If>
    </View>
  );
}

function ChangesBody(props: { review: GitReview; nav: PageNav; selected: string | null; onOpen: (entry: GitReviewEntry) => void }) {
  const review = props.review;
  const s = () => review.status();
  return (
    <View>
      {(() => {
        const snapshot = s();
        if (!snapshot && review.error()) {
          return <EmptyState testID="git-error" icon="alert" title="Couldn't load Git status" detail={review.error()} action="Retry" onAction={() => void review.refresh()} />;
        }
        if (!snapshot) return <EmptyState busy title="Inspecting repository" />;
        if (!snapshot.available) {
          const noCwd = snapshot.reason === "no_cwd";
          return (
            <EmptyState
              icon="git-branch"
              title={noCwd ? "No working directory yet" : "Not a Git repository"}
              detail={noCwd ? "This terminal hasn't reported a folder yet." : "Move this terminal into a Git repository, then refresh."}
              action="Refresh"
              onAction={() => void review.refresh()}
            />
          );
        }
        if (snapshot.clean) {
          return <EmptyState testID="git-clean" icon="check-circle" title="Working tree clean" detail={snapshot.branch} action="Browse files" onAction={() => props.nav.push({ name: "git-files", ...target(review) })} />;
        }
        const groups = review.groups();
        const query = review.query();
        const visible = (group: GitReviewGroup) => buildGitReviewRows(groups[group], { tree: review.tree(), collapsed: review.collapsed(), query, scope: group });
        if (query.trim() && !visible("staged").length && !visible("changes").length) return <EmptyState title="No matching files" />;
        return (
          <View>
            <If when={review.error()}><Notice tone="failed" title="Couldn't refresh Git status" detail={review.error()} action="Retry" onAction={() => void review.refresh()} /></If>
            <If when={groups.staged.length}><Group review={review} nav={props.nav} group="staged" rows={visible("staged")} entries={groups.staged} selected={props.selected} onOpen={props.onOpen} /></If>
            <Group review={review} nav={props.nav} group="changes" rows={visible("changes")} entries={groups.changes} selected={props.selected} onOpen={props.onOpen} />
          </View>
        );
      })()}
    </View>
  );
}

const GROUP_LABEL: Record<GitReviewGroup, string> = { staged: "Staged", changes: "Not staged" };

/** Staged, or Changes (unstaged and untracked): a header that folds the group, its rows, and the one button for all of them. */
function Group(props: { review: GitReview; nav: PageNav; group: GitReviewGroup; rows: GitReviewRow[]; entries: GitReviewEntry[]; selected: string | null; onOpen: (entry: GitReviewEntry) => void }) {
  const staged = props.group === "staged";
  const key = `section:${props.group}`;
  const collapsed = () => props.review.collapsed().has(key);
  const count = props.entries.length;
  const totals = sumGitReviewEntries(props.entries);
  const label = GROUP_LABEL[props.group];
  return (
    <View testID={`git-group-${props.group}`}>
      <SectionLabel
        testID={`git-group-${props.group}-toggle`}
        title={label}
        count={count}
        folded={collapsed()}
        accessibilityLabel={`${label}, ${count} ${count === 1 ? "file" : "files"}`}
        onPress={() => props.review.toggleCollapsed(key)}
        trailing={
          <View class="flex-row items-center gap-2">
            <Counts additions={totals.additions} deletions={totals.deletions} />
            <If when={count}><If when={props.review.isBusy(`bulk:${props.group}`)} else={<RowButton testID={`git-bulk-${props.group}`} label={staged ? "Unstage all" : "Stage all"} onPress={() => void props.review.bulk(props.group)} />}><Spinner size={16} /></If></If>
          </View>
        }
      />
      <If when={!collapsed()}><View class="overflow-hidden rounded-(--radius-group) bg-card">
          <View class="-mb-px">
            <If when={props.rows.length} else={<Row title="No changes" />}>{props.rows.map((row) => <ReviewRow review={props.review} nav={props.nav} row={row} selected={props.selected} onOpen={props.onOpen} />)}</If>
          </View>
        </View></If>
    </View>
  );
}

function ReviewRow(props: { review: GitReview; nav: PageNav; row: GitReviewRow; selected: string | null; onOpen: (entry: GitReviewEntry) => void }): JSX.Element {
  const row = props.row;
  const indent = { paddingLeft: 16 + row.depth * 16 };
  if (row.kind === "dir") {
    return (
      <View
        role="button"
        accessibilityLabel={`${row.label}, ${row.count} files`}
        accessibilityState={{ expanded: !row.collapsed }}
        onPress={() => props.review.toggleCollapsed(row.key)}
        class="min-h-[48px] flex-row items-center gap-2 py-2.5 pr-4 active:bg-accent"
        style={indent}
      >
        <Icon name={row.collapsed ? "chevron-right" : "chevron-down"} size={14} tone="faint" />
        <Icon name="folder" size={16} tone="ink" />
        <Text class="flex-1 font-mono text-code text-muted-foreground line-clamp-1">{row.label}</Text>
        <Text class="text-caption text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{String(row.count)}</Text>
        <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: 16 }} />
      </View>
    );
  }
  const entry = row.entry;
  const staged = entry.group === "staged";
  const name = splitGitReviewPath(entry.file.path).name;
  const word = gitDiffStatusWord(gitDiffTone(entry.file));
  return (
    <View
      testID={`git-row-${entry.key}`}
      role="button"
      accessibilityLabel={`${name}, ${word}, ${entry.additions} added, ${entry.deletions} removed, ${staged ? "staged" : "not staged"}`}
      accessibilityHint="Opens the diff"
      accessibilityState={{ selected: props.selected === entry.key }}
      onPress={() => props.onOpen(entry)}
      onLongPress={() => void entryMenu(props.review, props.nav, entry, props.onOpen)}
      // A mouse's right click (the web, the desktops) opens the same menu.
      onContextMenu={() => void entryMenu(props.review, props.nav, entry, props.onOpen)}
      class={`min-h-[56px] flex-row items-center gap-3 py-2.5 pr-1 active:bg-accent ${props.selected === entry.key ? "bg-accent" : ""}`}
      style={indent}
    >
      <StatusTile file={entry.file} />
      <View class="min-w-0 flex-1">
        <If when={props.review.tree()} else={<FileName path={entry.file.path} />}><Text class="font-mono text-code font-semibold line-clamp-1">{name}</Text></If>
        <If when={entry.file.old_path}>{(old_path) => <Text class="font-mono text-caption text-muted-foreground line-clamp-1">{`from ${old_path()}`}</Text>}</If>
      </View>
      <If when={entry.file.binary} else={<Counts additions={entry.additions} deletions={entry.deletions} />}><Text class="font-mono text-code text-muted-foreground">bin</Text></If>
      <IconButton
        testID={`git-toggle-${entry.key}`}
        icon={staged ? "remove-circle" : "add-circle"}
        label={`${staged ? "Unstage" : "Stage"} ${name}`}
        busy={props.review.isBusy(entry.key)}
        onPress={() => void props.review.toggleStaged(entry)}
      />
      <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: 16 + row.depth * 16 + 40 }} />
    </View>
  );
}

function HistoryBody(props: { review: GitReview; nav: PageNav }) {
  const review = props.review;
  return (
    <View>
      {(() => {
        const history = review.history();
        if (!history.commits.length && history.error) return <EmptyState icon="alert" title="Couldn't load history" detail={history.error} action="Retry" onAction={() => void review.loadHistory(0)} />;
        if (!history.commits.length) return <If when={history.loading || !history.loaded} else={<EmptyState title="No commits yet" />}><EmptyState busy title="Loading history" /></If>;
        // A grouped list (SPEC §15): the commits by day, each day's label with its count.
        return (
          <View>
            {gitCommitDays(history.commits).map((day) => (
              <Section label={<SectionLabel icon="time" title={day.label} count={day.commits.length} />}>
                {day.commits.map((commit) => <CommitRow review={review} nav={props.nav} commit={commit} />)}
              </Section>
            ))}
            <If when={history.moreError || history.hasMore}><Section>
                <If when={history.moreError}>{(moreError) => <Row title="Couldn't load more" subtitle={moreError()} />}</If>
                <If when={history.hasMore}><Row testID="git-history-more" title={history.loading ? "Loading history" : "Load more"} disabled={history.loading} onPress={() => void review.loadHistory(history.commits.length)} /></If>
              </Section></If>
          </View>
        );
      })()}
    </View>
  );
}

function CommitRow(props: { review: GitReview; nav: PageNav; commit: GitCommitSummary }) {
  const c = props.commit;
  const refs = (c.refs ?? []).slice(0, 2).join(" · ");
  return (
    <Row
      testID={`git-commit-${c.short}`}
      title={c.subject || "(no message)"}
      subtitle={[c.short, c.author, formatGitAge(c.date)].join(" · ")}
      accessibilityLabel={`${c.subject || "(no message)"}, ${c.author}, ${spokenGitAge(c.date)}`}
      trailing={<If when={refs}>{(refs) => <Tag label={refs()} />}</If>}
      chevron
      onPress={() => props.nav.push({ name: "git-commit", ...target(props.review), sha: c.sha })}
    />
  );
}

/** One commit: its message, who and when, and the files it changed, each opening its diff. */
function CommitPage(props: { review: GitReview; sha: string; nav: PageNav }) {
  const review = props.review;
  if (!review.commits()[props.sha]?.value) void review.loadCommit(props.sha);
  const summary = () => review.history().commits.find((c) => c.sha === props.sha);
  return (
    <Page title={summary()?.short ?? props.sha.slice(0, 7)} testID="git-commit-page">
      <Current review={review}>{() => (
        <View>
          {(() => {
            const state = review.commits()[props.sha];
            if (state?.error && !state.value) return <EmptyState icon="alert" title="Couldn't load this commit" detail={state.error} action="Retry" onAction={() => void review.loadCommit(props.sha)} />;
            const detail = state?.value;
            if (!detail) return <EmptyState busy title="Loading commit" />;
            return (
              <View>
                <DisplayTitle eyebrow={[detail.short, detail.author, formatGitAge(detail.date)].join(" · ")} title={detail.subject || "(no message)"} />
                <View class="gap-1 px-1">
                  <If when={detail.body.trim()}><Text class="text-body text-muted-foreground line-clamp-6">{detail.body.trim()}</Text></If>
                  <If when={detail.truncated}><Text class="text-sub">Large commit: the last files are not shown.</Text></If>
                </View>
                <Section title={`${detail.files.length} ${detail.files.length === 1 ? "file" : "files"}`}>
                  {detail.files.map((file) => (
                    <View
                      testID={`git-commit-file-${file.path}`}
                      role="button"
                      accessibilityLabel={`${splitGitReviewPath(file.path).name}, ${gitDiffStatusWord(gitDiffTone(file))}, ${file.additions} added, ${file.deletions} removed`}
                      onPress={() => props.nav.push({ name: "git-commit-file", ...target(review), sha: props.sha, path: file.path })}
                      class="min-h-[56px] flex-row items-center gap-3 px-4 py-2.5 active:bg-accent"
                    >
                      <StatusTile file={file} />
                      <View class="min-w-0 flex-1"><FileName path={file.path} /></View>
                      <If when={file.binary} else={<Counts additions={file.additions} deletions={file.deletions} />}><Text class="font-mono text-code text-muted-foreground">bin</Text></If>
                      <Icon name="chevron-right" size={16} tone="faint" />
                      <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: 56 }} />
                    </View>
                  ))}
                </Section>
              </View>
            );
          })()}
        </View>
      )}</Current>
    </Page>
  );
}
