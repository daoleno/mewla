import { signal, Text, View } from "nsh";
import { DiffView, Tree, type TreeItem } from "nsh/base";
import { NavPage } from "../../shell/NavPage";
import { formatGitDiffBytes } from "../../../core/git/gitDiffReview";
import { EmptyState, Notice } from "../kit/kit";
import { Icon } from "../kit/Icon";
import type { PageNav } from "../routes";
import { Current, IconButton } from "./parts";
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

/** Every file of the repository (tracked or not ignored), as a tree; a file opens as it is now. */
export function FilesPage(props: { review: GitReview; nav: PageNav }) {
  const review = props.review;
  const expanded = signal<string[]>([]);
  if (!review.paths().value) void review.loadPaths();
  let built: { paths: readonly string[]; items: TreeItem[] } | null = null;
  const items = () => {
    const paths = review.paths().value?.paths ?? [];
    if (built?.paths !== paths) built = { paths, items: repoTree(paths) };
    return built.items;
  };
  return (
    <NavPage
      title={review.status()?.repo_name || "Files"}
      testID="git-files-page"
      headerRight={<IconButton testID="git-files-refresh" icon="refresh" label="Refresh" onPress={() => void review.loadPaths()} />}
    >
      <Current review={review} class="flex-1 bg-background">{() => (
          <View class="flex-1 bg-background">
            {(() => {
              const state = review.paths();
              if (!state.value && state.error) return <View class="px-4"><EmptyState icon="alert" title="Couldn't load files" detail={state.error} action="Retry" onAction={() => void review.loadPaths()} /></View>;
              if (!state.value) return <View class="px-4"><EmptyState busy title="Loading files" /></View>;
              if (!state.value.paths.length) return <View class="px-4"><EmptyState title="No files" /></View>;
              return (
                <View class="flex-1">
                  {state.value.truncated ? <View class="px-4"><Notice tone="warning" title="Showing part of this repository" /></View> : null}
                  <Tree
                    testID="git-files-tree"
                    class="flex-1"
                    listClass="py-2"
                    accessibilityLabel="Repository files"
                    items={items()}
                    expanded={expanded()}
                    onExpandedChange={({ expanded: next }) => expanded.set(next)}
                    selected={null}
                    onSelectedChange={({ selected }) => {
                      if (!selected.startsWith(DIR)) props.nav.push({ name: "git-file", targetId: review.target.targetId, cwd: review.target.cwd, path: selected });
                    }}
                    itemClass="active:bg-accent"
                    renderItem={(entry) => (
                      <View testID={`git-file-${entry.id}`} class="flex-row items-center gap-2 py-2 pr-4" style={{ paddingLeft: 16 + entry.depth * 16 }}>
                        <Icon name={entry.folder ? (expanded().includes(entry.id) ? "folder-open" : "folder") : "document-text"} size={16} tone={entry.folder ? "soft" : "faint"} />
                        <Text class="flex-1 font-mono text-sm line-clamp-1">{entry.label}</Text>
                      </View>
                    )}
                  />
                </View>
              );
            })()}
          </View>
      )}</Current>
    </NavPage>
  );
}

/** One file of the working tree, as it is now, read only. */
export function FilePage(props: { review: GitReview; path: string; nav: PageNav }) {
  const review = props.review;
  if (!review.files()[props.path]?.value) void review.loadFile(props.path);
  return (
    <NavPage title={props.path || "Files"} testID="git-file-page" headerRight={<IconButton icon="link" label="Copy path" onPress={() => void copyText(props.path, "Path copied")} />}>
      <Current review={review} class="flex-1 bg-background">{() => (
          <View class="flex-1 bg-background">
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
