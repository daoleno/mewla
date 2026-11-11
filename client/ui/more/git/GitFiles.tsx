import { signal, Text, View } from "nsh";
import { AlertDialog, DiffView, Tree, type TreeItem } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { NavPage } from "../../shell/NavPage";
import type { GitDiffFileInfo } from "../../../core/git/gitDiff";
import { gitDiffStatusWord, gitDiffTone } from "../../../core/git/gitDiffPresentation";
import { formatGitDiffBytes } from "../../../core/git/gitDiffReview";
import { matchesGitReviewQuery } from "../../../core/git/gitReviewModel";
import { EmptyState, Field, Notice } from "../kit/kit";
import { Icon } from "../kit/Icon";
import type { PageNav } from "../routes";
import { Current, FlashLine, IconButton, StatusChar } from "./parts";
import { copyText, type GitReview } from "./state";

const DIR = "dir:";

/** The repository's paths as folders and files, folders first, each level by name. */
export function repoTree(paths: readonly string[]): TreeItem[] {
  interface Node { item: TreeItem; dirs: Map<string, Node>; files: TreeItem[] }
  const root: Node = { item: { id: DIR, label: "" }, dirs: new Map(), files: [] };
  for (const path of paths) {
    const parts = path.split("/");
    let node = root;
    let prefix = "";
    for (const part of parts.slice(0, -1)) {
      prefix = prefix ? `${prefix}/${part}` : part;
      let child = node.dirs.get(part);
      if (!child) {
        child = { item: { id: `${DIR}${prefix}`, label: part }, dirs: new Map(), files: [] };
        node.dirs.set(part, child);
      }
      node = child;
    }
    node.files.push({ id: path, label: parts[parts.length - 1] });
  }
  const build = (node: Node): TreeItem[] => [
    ...[...node.dirs.values()].sort((a, b) => a.item.label.localeCompare(b.item.label)).map((dir) => ({ ...dir.item, children: build(dir) })),
    ...node.files.sort((a, b) => a.label.localeCompare(b.label)),
  ];
  return build(root);
}

/** Every folder id of a tree, for a search that shows what it found unfolded. */
export function repoTreeFolders(items: readonly TreeItem[]): string[] {
  return items.flatMap((item) => (item.children?.length ? [item.id, ...repoTreeFolders(item.children)] : []));
}

/** The changed files of the status by path, and the folders that hold one, for the tree's marks. */
export function repoTreeChanges(files: readonly GitDiffFileInfo[]): { files: Map<string, GitDiffFileInfo>; folders: Set<string> } {
  const byPath = new Map<string, GitDiffFileInfo>();
  const folders = new Set<string>();
  for (const file of files) {
    byPath.set(file.path, file);
    const parts = file.path.split("/");
    for (let i = 1; i < parts.length; i++) folders.add(`${DIR}${parts.slice(0, i).join("/")}`);
  }
  return { files: byPath, folders };
}

/** What a long press on an entry offers: open a file, copy the path of a file or a folder. */
async function entryMenu(review: GitReview, nav: PageNav, id: string): Promise<void> {
  const folder = id.startsWith(DIR);
  const path = folder ? id.slice(DIR.length) : id;
  const answer = await AlertDialog.show({
    title: path,
    presentation: "actionSheet",
    actions: [
      ...(folder ? [] : [{ id: "open", label: "Open file" }]),
      { id: "copy", label: "Copy path" },
      { id: "cancel", label: "Cancel", role: "cancel" as const },
    ],
  });
  if (answer.action === "open") nav.push({ name: "git-file", targetId: review.target.targetId, cwd: review.target.cwd, path });
  else if (answer.action === "copy") void copyText(path, "Path copied", path);
}

/**
 * Every file of the repository (tracked or not ignored), as a tree with the changed
 * ones marked, searchable; a file opens as it is now, a long press copies a path.
 */
export function FilesPage(props: { review: GitReview; nav: PageNav }) {
  const review = props.review;
  const expanded = signal<string[]>([]);
  const query = signal("");
  if (!review.paths().value) void review.loadPaths();
  let built: { paths: readonly string[]; query: string; items: TreeItem[] } | null = null;
  const items = () => {
    const paths = review.paths().value?.paths ?? [];
    const q = query();
    if (built?.paths !== paths || built.query !== q) built = { paths, query: q, items: repoTree(q.trim() ? paths.filter((path) => matchesGitReviewQuery(path, q)) : paths) };
    return built.items;
  };
  const changes = () => repoTreeChanges(review.status()?.files ?? []);
  // While searching every folder of what was found is open; the person's own folding comes back after.
  const shownExpanded = () => (query().trim() ? repoTreeFolders(items()) : expanded());
  const openFile = (path: string) => props.nav.push({ name: "git-file", targetId: review.target.targetId, cwd: review.target.cwd, path });
  // On a phone a row that hears long presses (its menu) takes the touch from the tree, so it hears the tap
  // too: a folder opens or closes (a search shows every folder open), a file opens.
  const tapped = (id: string, folder: boolean) => {
    if (!folder) return openFile(id);
    if (query().trim()) return;
    expanded.set(expanded().includes(id) ? expanded().filter((open) => open !== id) : [...expanded(), id]);
  };
  return (
    <NavPage
      title={review.status()?.repo_name || "Files"}
      testID="git-files-page"
      headerRight={<IconButton testID="git-files-refresh" icon="refresh" label="Refresh" onPress={() => void review.loadPaths()} />}
    >
      <Current review={review} class="flex-1 bg-background">{() => (
          <View class="flex-1 bg-background">
            <View class="px-4"><FlashLine /></View>
            {(() => {
              const state = review.paths();
              if (!state.value && state.error) return <View class="px-4"><EmptyState icon="alert" title="Couldn't load files" detail={state.error} action="Retry" onAction={() => void review.loadPaths()} /></View>;
              if (!state.value) return <View class="px-4"><EmptyState busy title="Loading files" /></View>;
              if (!state.value.paths.length) return <View class="px-4"><EmptyState title="No files" /></View>;
              return (
                <View class="flex-1">
                  {state.value.truncated ? <View class="px-4"><Notice tone="warning" title="Showing part of this repository" /></View> : null}
                  <Field testID="git-files-query" accessibilityLabel="Search files" placeholder="Search files" value={query()} onChange={(value) => query.set(value)} />
                  {query().trim() && !items().length ? <View class="px-4"><EmptyState title="No matching files" /></View> : null}
                  <Tree
                    testID="git-files-tree"
                    class="flex-1"
                    listClass="py-2"
                    accessibilityLabel="Repository files"
                    items={items()}
                    expanded={shownExpanded()}
                    onExpandedChange={({ expanded: next }) => {
                      if (!query().trim()) expanded.set(next);
                    }}
                    selected={null}
                    onSelectedChange={({ selected }) => {
                      if (!selected.startsWith(DIR)) openFile(selected);
                    }}
                    itemClass="active:bg-accent"
                    renderItem={(entry) => {
                      const changed = entry.folder ? null : changes().files.get(entry.id) ?? null;
                      const holds = entry.folder && changes().folders.has(entry.id);
                      return (
                        <View
                          testID={`git-file-${entry.id}`}
                          onPress={mobile ? () => tapped(entry.id, entry.folder) : undefined}
                          onLongPress={() => void entryMenu(review, props.nav, entry.id)}
                          onContextMenu={() => void entryMenu(review, props.nav, entry.id)}
                          accessibilityLabel={`${entry.label}${changed ? `, ${gitDiffStatusWord(gitDiffTone(changed))}` : holds ? ", has changes" : ""}`}
                          class="flex-row items-center gap-2 py-2 pr-4"
                          style={{ paddingLeft: 16 + entry.depth * 16 }}
                        >
                          <Icon name={entry.folder ? (shownExpanded().includes(entry.id) ? "folder-open" : "folder") : "document-text"} size={16} tone={entry.folder ? "soft" : "faint"} />
                          <Text class={`flex-1 font-mono text-sm line-clamp-1 ${changed || holds ? "font-semibold" : ""}`}>{entry.label}</Text>
                          {changed ? <StatusChar file={changed} /> : holds ? <View testID={`git-file-dot-${entry.id}`} class="size-1.5 rounded-full bg-foreground" /> : null}
                        </View>
                      );
                    }}
                  />
                </View>
              );
            })()}
          </View>
      )}</Current>
    </NavPage>
  );
}

/** One file of the working tree, as it is now, read only; read again each time it opens and on Refresh. */
export function FilePage(props: { review: GitReview; path: string; nav: PageNav }) {
  const review = props.review;
  void review.loadFile(props.path);
  return (
    <NavPage
      title={props.path || "Files"}
      testID="git-file-page"
      headerRight={
        <View class="flex-row items-center">
          <IconButton testID="git-file-refresh" icon="refresh" label="Refresh" busy={!!review.files()[props.path]?.loading && !!review.files()[props.path]?.value} onPress={() => void review.loadFile(props.path)} />
          <IconButton testID="git-file-copy-path" icon="link" label="Copy path" onPress={() => void copyText(props.path, "Path copied", props.path)} />
        </View>
      }
    >
      <Current review={review} class="flex-1 bg-background">{() => (
          <View class="flex-1 bg-background">
            <View class="px-4"><FlashLine /></View>
            {(() => {
              const state = review.files()[props.path];
              if (state?.error && !state.value) return <View class="px-4"><EmptyState icon="alert" title="Couldn't load file" detail={state.error} action="Retry" onAction={() => void review.loadFile(props.path)} /></View>;
              const snapshot = state?.value?.snapshot;
              if (!snapshot) return <View class="px-4"><EmptyState busy title="Loading file" /></View>;
              if (!snapshot.exists) return <View class="px-4"><EmptyState title="File snapshot unavailable" detail={snapshot.reason || "This file could not be read from the working tree."} /></View>;
              if (snapshot.binary) return <View class="px-4"><EmptyState title="Binary file" detail={formatGitDiffBytes(snapshot.byte_count)} /></View>;
              const content = snapshot.content ?? "";
              if (!content) return <View class="px-4"><EmptyState title="Empty file" /></View>;
              return (
                <View class="flex-1">
                  {snapshot.truncated ? (
                    <View class="px-4">
                      <Notice tone="warning" title="Showing part of this file" detail={`First ${formatGitDiffBytes(content.length)} of ${formatGitDiffBytes(snapshot.byte_count)}.`} />
                    </View>
                  ) : null}
                  <DiffView testID="git-file-view" class="flex-1" oldValue={content} newValue={content} expandAll language={props.path} layout="unified" />
                </View>
              );
            })()}
          </View>
      )}</Current>
    </NavPage>
  );
}
