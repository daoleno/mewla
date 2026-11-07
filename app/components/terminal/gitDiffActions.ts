import type { ActionMenuItem } from "../ui/ActionMenu";

export interface GitDiffMenuRequest {
  title?: string;
  items: ActionMenuItem[];
}

/** Secondary actions for one changed file, shared by header and row menus. */
export function buildGitDiffFileActions({
  path,
  deleted,
  reading,
  searchOpen,
  optionsOpen,
  onToggleSearch,
  onToggleOptions,
  onViewDiff,
  onOpenFile,
  onCopyPath,
  onCopyPatch,
}: {
  path: string;
  deleted: boolean;
  /** Reader-only controls (find, display) appear when the diff is open. */
  reading: boolean;
  searchOpen?: boolean;
  optionsOpen?: boolean;
  onToggleSearch?(): void;
  onToggleOptions?(): void;
  onViewDiff?(): void;
  onOpenFile(path: string): void;
  onCopyPath(path: string): void;
  onCopyPatch(path: string): void;
}): ActionMenuItem[] {
  const items: ActionMenuItem[] = [];
  if (!reading && onViewDiff) {
    items.push({ key: "view", label: "View diff", icon: "git-compare", onPress: onViewDiff });
  }
  if (reading && onToggleSearch) {
    items.push({
      key: "find",
      label: searchOpen ? "Hide find bar" : "Find in diff",
      icon: "search",
      onPress: onToggleSearch,
    });
  }
  if (reading && onToggleOptions) {
    items.push({
      key: "display",
      label: optionsOpen ? "Hide display options" : "Display options",
      icon: "text",
      onPress: onToggleOptions,
    });
  }
  items.push(
    {
      key: "open",
      label: "Open working file",
      detail: deleted ? "Deleted from the working tree" : undefined,
      icon: "document-text",
      disabled: deleted,
      onPress: () => onOpenFile(path),
    },
    { key: "copy-path", label: "Copy path", icon: "link", onPress: () => onCopyPath(path) },
    { key: "copy-diff", label: "Copy diff", icon: "copy", onPress: () => onCopyPatch(path) },
  );
  return items;
}
