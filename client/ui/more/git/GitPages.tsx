import { effect, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { AlertDialog, Button } from "nsh/base";
import type { GitCommitSummary } from "../../../core/git/gitDiff";
import { buildGitReviewRows, gitReviewSummary, gitReviewSync, gitSyncAction, type GitReviewEntry, type GitReviewGroup, type GitReviewRow } from "../../../core/git/gitReviewModel";
import { formatGitAge } from "../../../core/git/gitReviewTime";
import { EmptyState, Field, Notice, Page, Row, Section, Tag } from "../kit/kit";
import { Icon } from "../kit/Icon";
import { Segmented } from "../kit/Segmented";
import type { PageNav } from "../routes";
import { CommitFilePage, DiffPage } from "./GitDiff";
import { FilePage, FilesPage } from "./GitFiles";
import { Counts, FlashLine, IconButton, PathText, StatusChar, TextButton } from "./parts";
import type { GitPage } from "./routes";
import { BranchSheet, CommitSheet } from "./sheets";
import { Current } from "./parts";
import { copyText, gitReview, type GitReview } from "./state";

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

export function openDiff(review: GitReview, nav: PageNav, entry: GitReviewEntry): void {
  nav.push({ name: "git-diff", ...target(review), entry: entry.key });
}

export function openWorkingFile(review: GitReview, nav: PageNav, path: string): void {
  nav.push({ name: "git-file", ...target(review), path });
}

/** The row menu: what a long press on a changed file offers. */
async function entryMenu(review: GitReview, nav: PageNav, entry: GitReviewEntry): Promise<void> {
  const staged = entry.group === "staged";
  const answer = await AlertDialog.show({
    title: entry.file.path,
    presentation: "actionSheet",
    actions: [
      { id: "view", label: "View diff" },
      { id: "stage", label: staged ? "Unstage" : "Stage" },
      ...(staged ? [] : [{ id: "discard", label: "Discard changes", role: "destructive" as const }]),
      ...(entry.file.status === "deleted" ? [] : [{ id: "open", label: "Open working file" }]),
      { id: "copy", label: "Copy path" },
      { id: "cancel", label: "Cancel", role: "cancel" as const },
    ],
  });
  if (answer.action === "view") openDiff(review, nav, entry);
  else if (answer.action === "stage") void review.toggleStaged(entry);
  else if (answer.action === "discard") void review.discardFile(entry);
  else if (answer.action === "open") openWorkingFile(review, nav, entry.file.path);
  else if (answer.action === "copy") void copyText(entry.file.path, "Path copied");
}

/** More actions on the whole repository. */
async function overflowMenu(review: GitReview, nav: PageNav): Promise<void> {
  const snapshot = review.status();
  const actions: Array<{ id: string; label: string; role?: "destructive" | "cancel" }> = [{ id: "refresh", label: "Refresh" }];
  if (snapshot?.available) {
    actions.push({ id: "browse", label: "Browse files" });
    if (snapshot.upstream) actions.push({ id: "fetch", label: "Fetch" }, { id: "pull", label: "Pull" }, { id: "push", label: "Push" });
    if (review.lastDiscard()) actions.push({ id: "undo", label: "Undo last discard" });
    if (review.groups().changes.length) actions.push({ id: "discard-all", label: "Discard all changes", role: "destructive" });
    if (snapshot.repo_root) actions.push({ id: "copy-root", label: "Copy repository path" });
    if (snapshot.branch) actions.push({ id: "copy-branch", label: "Copy branch name" });
  }
  actions.push({ id: "cancel", label: "Cancel", role: "cancel" });
  const answer = await AlertDialog.show({ title: snapshot?.repo_name || undefined, presentation: "actionSheet", actions });
  switch (answer.action) {
    case "refresh":
      void review.refresh();
      review.reloadHistory();
      break;
    case "browse":
      nav.push({ name: "git-files", ...target(review) });
      break;
    case "fetch":
    case "pull":
    case "push":
      void review.sync(answer.action);
      break;
    case "undo":
      review.undoLastDiscard();
      break;
    case "discard-all":
      void review.discardAll();
      break;
    case "copy-root":
      void copyText(snapshot!.repo_root!, "Path copied");
      break;
    case "copy-branch":
      void copyText(snapshot!.branch!, "Branch copied");
      break;
  }
}

type Tab = "changes" | "history";

/** Git review: what changed (staged and not), the history, the branch and how it syncs. */
function ReviewPage(props: { review: GitReview; title?: string; nav: PageNav }) {
  const review = props.review;
  const tab = signal<Tab>("changes");
  const commitOpen = signal(false);
  const branchesOpen = signal(false);
  review.open();
  // History loads the first time its tab is shown.
  effect(() => {
    if (tab() === "history") untracked(() => !review.history().loaded && !review.history().loading && void review.loadHistory(0));
  });
  const snapshot = () => review.status();
  const title = () => snapshot()?.repo_name || props.title || "Git";
  const headerRight = (
    <View class="flex-row items-center">
      <IconButton testID="git-search" icon="search" label="Search changed files" selected={review.searchOpen()} onPress={() => { review.searchOpen.set(!review.searchOpen()); if (!review.searchOpen()) review.query.set(""); }} />
      <IconButton testID="git-tree" icon={review.tree() ? "list" : "folder"} label={review.tree() ? "Show as list" : "Show as folders"} onPress={() => review.setTree(!review.tree())} />
      <IconButton testID="git-more" icon="more-vertical" label="More actions" onPress={() => void overflowMenu(review, props.nav)} />
    </View>
  );
  return (
    <Page title={title()} testID="git-page" headerRight={headerRight} refreshing={review.loading() && !!snapshot()} onRefresh={() => { void review.refresh(); review.reloadHistory(); }}>
      <Current review={review}>{() => (
        <View>
          <FlashLine />
          <View class="mt-3">
            <Segmented testID="git-tab" value={tab()} onChange={(next) => tab.set(next)} options={[{ key: "changes", label: "Changes" }, { key: "history", label: "History" }]} />
          </View>
          <Strip review={review} tab={tab()} onBranches={() => branchesOpen.set(true)} />
          {review.searchOpen() && tab() === "changes" ? (
            <View class="-mx-4">
              <Field testID="git-query" accessibilityLabel="Filter files" placeholder="Filter files" value={review.query()} onChange={(value) => review.query.set(value)} />
            </View>
          ) : null}
          <View>{tab() === "changes" ? <ChangesBody review={review} nav={props.nav} onCommit={() => commitOpen.set(true)} /> : <HistoryBody review={review} nav={props.nav} />}</View>
          <CommitSheet review={review} open={commitOpen()} count={review.groups().staged.length} canPush={!!snapshot()?.available && !snapshot()?.detached} onClose={() => commitOpen.set(false)} />
          <BranchSheet review={review} open={branchesOpen()} onClose={() => branchesOpen.set(false)} />
        </View>
      )}</Current>
    </Page>
  );
}

/** The branch, what changed in all, how it syncs, and a merge or rebase in progress. */
function Strip(props: { review: GitReview; tab: Tab; onBranches: () => void }) {
  const snapshot = () => props.review.status();
  return (
    <View>
      {snapshot()?.available ? (
        <View class="mt-3 flex-row flex-wrap items-center gap-2 px-1">
          {(() => {
            const s = snapshot()!;
            const label = s.detached ? `Detached ${s.branch ?? ""}`.trim() : s.branch || "No branch";
            return (
              <Button testID="git-branch" accessibilityLabel={`Branch ${label}. Switch branch`} onPress={props.onBranches} class="flex-row items-center gap-1.5 rounded-full bg-secondary px-3 py-1.5 active:opacity-70">
                <Icon name="git-branch" size={16} tone="ink" />
                <Text class="font-mono text-sm line-clamp-1">{label}</Text>
              </Button>
            );
          })()}
          <Text testID="git-summary" class="flex-1 text-sm text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>
            {props.tab === "changes" ? gitReviewSummary(snapshot()) : gitReviewSync(snapshot())}
          </Text>
          {(() => {
            const sync = gitSyncAction(snapshot());
            return sync ? <TextButton testID="git-sync" label={sync.label} busy={props.review.isBusy("sync")} onPress={() => void props.review.sync(sync.kind)} /> : null;
          })()}
        </View>
      ) : null}
      {snapshot()?.operation ? <Text class="mt-2 px-1 text-sm text-destructive">{`${snapshot()!.operation} in progress`}</Text> : null}
    </View>
  );
}

function ChangesBody(props: { review: GitReview; nav: PageNav; onCommit: () => void }) {
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
            {review.error() ? <Notice tone="failed" title="Couldn't refresh Git status" detail={review.error()} action="Retry" onAction={() => void review.refresh()} /> : null}
            {groups.staged.length ? <Group review={review} nav={props.nav} group="staged" rows={visible("staged")} count={groups.staged.length} /> : null}
            <Group review={review} nav={props.nav} group="changes" rows={visible("changes")} count={groups.changes.length} />
            <View class="mt-4">
              {groups.staged.length ? (
                <TextButton testID="git-commit-open" filled label={`Commit ${groups.staged.length} ${groups.staged.length === 1 ? "file" : "files"}`} onPress={props.onCommit} />
              ) : groups.changes.length ? (
                <TextButton testID="git-stage-all" label={`Stage all ${groups.changes.length}`} busy={review.isBusy("bulk:changes")} onPress={() => void review.bulk("changes")} />
              ) : null}
            </View>
          </View>
        );
      })()}
    </View>
  );
}

/** Staged, or Changes (unstaged and untracked): its rows and the one button for all of them. */
function Group(props: { review: GitReview; nav: PageNav; group: GitReviewGroup; rows: GitReviewRow[]; count: number }) {
  const staged = props.group === "staged";
  const detail = props.count ? (
    <Button testID={`git-bulk-${props.group}`} onPress={() => void props.review.bulk(props.group)} disabled={props.review.isBusy(`bulk:${props.group}`)} class="active:opacity-60 disabled:opacity-50">
      <Text class="text-sm font-medium">{staged ? "Unstage all" : "Stage all"}</Text>
    </Button>
  ) : undefined;
  return (
    <Section title={`${staged ? "Staged" : "Changes"} · ${props.count}`} detail={detail} testID={`git-group-${props.group}`}>
      {props.rows.length ? props.rows.map((row) => <ReviewRow review={props.review} nav={props.nav} row={row} />) : <Row title="No changes" />}
    </Section>
  );
}

function ReviewRow(props: { review: GitReview; nav: PageNav; row: GitReviewRow }): JSX.Element {
  const row = props.row;
  const indent = { paddingLeft: 16 + row.depth * 16 };
  if (row.kind === "dir") {
    return (
      <View role="button" accessibilityLabel={`${row.label}, ${row.count} files`} onPress={() => {
        const next = new Set(props.review.collapsed());
        if (next.has(row.key)) next.delete(row.key);
        else next.add(row.key);
        props.review.collapsed.set(next);
      }} class="flex-row items-center gap-2 border-b border-border py-2.5 pr-4 active:bg-accent" style={indent}>
        <Icon name={row.collapsed ? "chevron-right" : "chevron-down"} size={14} tone="faint" />
        <Icon name="folder" size={16} tone="soft" />
        <Text class="flex-1 font-mono text-sm text-muted-foreground line-clamp-1">{row.label}</Text>
        <Text class="text-xs text-muted-foreground">{String(row.count)}</Text>
      </View>
    );
  }
  const entry = row.entry;
  const staged = entry.group === "staged";
  const name = entry.file.path.slice(entry.file.path.lastIndexOf("/") + 1);
  return (
    <View
      testID={`git-row-${entry.key}`}
      role="button"
      accessibilityLabel={entry.file.path}
      accessibilityHint="Opens the diff"
      onPress={() => openDiff(props.review, props.nav, entry)}
      onLongPress={() => void entryMenu(props.review, props.nav, entry)}
      class="flex-row items-center gap-2 border-b border-border py-2.5 pr-2 active:bg-accent"
      style={indent}
    >
      <StatusChar file={entry.file} />
      <View class="min-w-0 flex-1">
        {props.review.tree() ? <Text class="font-mono text-sm line-clamp-1">{name}</Text> : <PathText path={entry.file.path} />}
        {entry.file.old_path ? <Text class="font-mono text-xs text-muted-foreground line-clamp-1">{`from ${entry.file.old_path}`}</Text> : null}
      </View>
      <Counts additions={entry.additions} deletions={entry.deletions} binary={entry.file.binary} />
      <IconButton
        testID={`git-toggle-${entry.key}`}
        icon={staged ? "remove-circle" : "add-circle"}
        label={`${staged ? "Unstage" : "Stage"} ${name}`}
        busy={props.review.isBusy(entry.key)}
        onPress={() => void props.review.toggleStaged(entry)}
      />
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
        if (!history.commits.length) return history.loading || !history.loaded ? <EmptyState busy title="Loading history" /> : <EmptyState title="No commits yet" />;
        return (
          <Section>
            {history.commits.map((commit) => <CommitRow review={review} nav={props.nav} commit={commit} />)}
            {history.moreError ? <Row title="Couldn't load more" subtitle={history.moreError} /> : null}
            {history.hasMore ? <Row testID="git-history-more" title={history.loading ? "Loading history" : "Load more"} disabled={history.loading} onPress={() => void review.loadHistory(history.commits.length)} /> : null}
          </Section>
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
      title={c.subject}
      subtitle={[c.short, c.author, formatGitAge(c.date)].join(" · ")}
      trailing={refs ? <Tag label={refs} /> : undefined}
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
                <View class="mt-4 gap-1 px-1">
                  <Text class="text-lg font-semibold">{detail.subject}</Text>
                  {detail.body.trim() ? <Text class="text-sm text-muted-foreground line-clamp-6">{detail.body.trim()}</Text> : null}
                  <Text class="text-xs text-muted-foreground">{[detail.short, detail.author, formatGitAge(detail.date)].join(" · ")}</Text>
                  {detail.truncated ? <Text class="text-xs text-destructive">Large commit: the last files are not shown.</Text> : null}
                </View>
                <Section title={`${detail.files.length} ${detail.files.length === 1 ? "file" : "files"}`}>
                  {detail.files.map((file) => (
                    <View
                      testID={`git-commit-file-${file.path}`}
                      role="button"
                      accessibilityLabel={file.path}
                      onPress={() => props.nav.push({ name: "git-commit-file", ...target(review), sha: props.sha, path: file.path })}
                      class="flex-row items-center gap-2 border-b border-border px-4 py-3 active:bg-accent"
                    >
                      <StatusChar file={file} />
                      <View class="min-w-0 flex-1"><PathText path={file.path} /></View>
                      <Counts additions={file.additions} deletions={file.deletions} binary={file.binary} />
                      <Icon name="chevron-right" size={16} tone="faint" />
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
