import React, { useMemo } from "react";
import { FlatList, Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { TouchTarget, TypeScale, UiTextMetrics } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import { EmptyState } from "../ui/EmptyState";
import { GitDelta, GitIconButton, GitStatusMark, GitTextButton } from "./GitReviewParts";
import { gitDiffStatusWord, gitDiffTone, splitGitDiffPath } from "./gitDiffPresentation";
import {
  buildGitReviewRows,
  sumGitReviewEntries,
  type GitReviewEntry,
  type GitReviewGroup,
  type GitReviewGroups,
  type GitReviewRow,
} from "./gitReviewModel";

export const GIT_ROW_HEIGHT = 40;
const SECTION_HEIGHT = 36;
const DIR_HEIGHT = 34;

type ListItem =
  | { kind: "section"; key: string; group: GitReviewGroup; count: number; additions: number; deletions: number; collapsed: boolean }
  | GitReviewRow;

const GROUP_LABEL: Record<GitReviewGroup, string> = { staged: "Staged", changes: "Changes" };

interface GitReviewListProps {
  groups: GitReviewGroups;
  tree: boolean;
  query: string;
  collapsed: ReadonlySet<string>;
  selectedKey: string | null;
  busyKeys: ReadonlySet<string>;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  bottomInset: number;
  refreshing: boolean;
  onRefresh(): void;
  onOpen(entry: GitReviewEntry): void;
  onToggleStaged(entry: GitReviewEntry): void;
  onMenu(entry: GitReviewEntry): void;
  onToggleCollapsed(key: string): void;
  onBulk(group: GitReviewGroup): void;
  empty: React.ReactNode;
}

/**
 * Changed files in two groups, Staged and Changes. A row is one line (mark,
 * name with its folder, +/−, and the stage toggle) so a phone shows a screenful;
 * the tree mode nests the same rows by folder.
 */
export function GitReviewList({
  groups,
  tree,
  query,
  collapsed,
  selectedKey,
  busyKeys,
  theme,
  chrome,
  bottomInset,
  refreshing,
  onRefresh,
  onOpen,
  onToggleStaged,
  onMenu,
  onToggleCollapsed,
  onBulk,
  empty,
}: GitReviewListProps) {
  const items = useMemo(() => {
    const out: ListItem[] = [];
    for (const group of ["staged", "changes"] as const) {
      const entries = groups[group];
      if (!entries.length) continue;
      const rows = buildGitReviewRows(entries, { tree, collapsed, query, scope: group });
      if (query && !rows.length) continue;
      const sectionKey = `section:${group}`;
      const sectionCollapsed = collapsed.has(sectionKey);
      const totals = sumGitReviewEntries(entries);
      out.push({
        kind: "section",
        key: sectionKey,
        group,
        count: entries.length,
        additions: totals.additions,
        deletions: totals.deletions,
        collapsed: sectionCollapsed,
      });
      if (!sectionCollapsed) out.push(...rows);
    }
    return out;
  }, [groups, tree, query, collapsed]);

  const layout = useMemo(() => {
    const offsets: number[] = [];
    let offset = 0;
    for (const item of items) {
      offsets.push(offset);
      offset += itemHeight(item);
    }
    return offsets;
  }, [items]);

  if (!items.length) {
    return <View style={styles.empty}>{query ? <EmptyState size="inline" title="No matching files" /> : empty}</View>;
  }

  return (
    <FlatList
      data={items}
      keyExtractor={(item) => item.key}
      getItemLayout={(_, index) => ({ length: itemHeight(items[index]), offset: layout[index], index })}
      refreshing={refreshing}
      onRefresh={onRefresh}
      contentContainerStyle={{ paddingBottom: bottomInset + 8 }}
      keyboardShouldPersistTaps="handled"
      initialNumToRender={24}
      windowSize={9}
      renderItem={({ item }) => {
        if (item.kind === "section") {
          return (
            <SectionHeader
              item={item}
              theme={theme}
              chrome={chrome}
              onToggle={() => onToggleCollapsed(item.key)}
              onBulk={() => onBulk(item.group)}
              busy={busyKeys.has(`bulk:${item.group}`)}
            />
          );
        }
        if (item.kind === "dir") {
          return <DirRow row={item} chrome={chrome} onToggle={() => onToggleCollapsed(item.key)} />;
        }
        return (
          <FileRow
            entry={item.entry}
            depth={item.depth}
            tree={tree}
            selected={item.entry.key === selectedKey}
            busy={busyKeys.has(item.entry.key)}
            theme={theme}
            chrome={chrome}
            onOpen={() => onOpen(item.entry)}
            onToggleStaged={() => onToggleStaged(item.entry)}
            onMenu={() => onMenu(item.entry)}
          />
        );
      }}
    />
  );
}

function itemHeight(item: ListItem): number {
  return item.kind === "section" ? SECTION_HEIGHT : item.kind === "dir" ? DIR_HEIGHT : GIT_ROW_HEIGHT;
}

function SectionHeader({
  item,
  theme,
  chrome,
  busy,
  onToggle,
  onBulk,
}: {
  item: Extract<ListItem, { kind: "section" }>;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  busy: boolean;
  onToggle(): void;
  onBulk(): void;
}) {
  const label = GROUP_LABEL[item.group];
  return (
    <View style={[styles.section, { backgroundColor: chrome.appBackground, borderBottomColor: chrome.border }]}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${label}, ${item.count} files`}
        accessibilityState={{ expanded: !item.collapsed }}
        onPress={onToggle}
        style={styles.sectionMain}
      >
        <Icon name={item.collapsed ? "chevron-right" : "chevron-down"} size={14} color={chrome.textSubtle} />
        <Text style={[styles.sectionTitle, { color: chrome.text }]}>{label}</Text>
        <Text style={[styles.sectionCount, { color: chrome.textSubtle }]}>{item.count}</Text>
        <GitDelta additions={item.additions} deletions={item.deletions} theme={theme} chrome={chrome} style={styles.sectionDelta} />
      </Pressable>
      <GitTextButton
        label={item.group === "staged" ? "Unstage all" : "Stage all"}
        chrome={chrome}
        busy={busy}
        onPress={onBulk}
      />
    </View>
  );
}

function DirRow({ row, chrome, onToggle }: { row: Extract<GitReviewRow, { kind: "dir" }>; chrome: TerminalThemeChrome; onToggle(): void }) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${row.label}, ${row.count} files`}
      accessibilityState={{ expanded: !row.collapsed }}
      onPress={onToggle}
      style={[styles.dir, { paddingLeft: 12 + row.depth * 14 }]}
    >
      <Icon name={row.collapsed ? "chevron-right" : "chevron-down"} size={13} color={chrome.textSubtle} />
      <Text numberOfLines={1} ellipsizeMode="head" style={[styles.dirName, { color: chrome.textMuted }]}>
        {row.label}
      </Text>
      <Text style={[styles.dirCount, { color: chrome.textSubtle }]}>{row.count}</Text>
    </Pressable>
  );
}

const FileRow = React.memo(function FileRow({
  entry,
  depth,
  tree,
  selected,
  busy,
  theme,
  chrome,
  onOpen,
  onToggleStaged,
  onMenu,
}: {
  entry: GitReviewEntry;
  depth: number;
  tree: boolean;
  selected: boolean;
  busy: boolean;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  onOpen(): void;
  onToggleStaged(): void;
  onMenu(): void;
}) {
  const { file } = entry;
  const { directory, name } = splitGitDiffPath(file.path);
  const staged = entry.group === "staged";
  const word = gitDiffStatusWord(gitDiffTone(file));
  return (
    <View
      style={[
        styles.row,
        { paddingLeft: 12 + depth * 14, backgroundColor: selected ? chrome.surfaceActive : "transparent" },
      ]}
    >
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${name}, ${word}, ${entry.additions} added, ${entry.deletions} removed, ${staged ? "staged" : "not staged"}`}
        accessibilityHint="Opens the diff"
        onPress={onOpen}
        onLongPress={onMenu}
        style={({ pressed }) => [styles.rowMain, { opacity: pressed ? 0.6 : 1 }]}
      >
        <GitStatusMark file={file} theme={theme} chrome={chrome} />
        <Text numberOfLines={1} style={styles.rowName}>
          <Text style={[styles.rowNameText, { color: chrome.text }]}>{name}</Text>
          {tree || !directory ? null : <Text style={[styles.rowDir, { color: chrome.textSubtle }]}>{`  ${directory}`}</Text>}
        </Text>
        <GitDelta additions={entry.additions} deletions={entry.deletions} binary={file.binary} theme={theme} chrome={chrome} />
      </Pressable>
      <GitIconButton
        icon={staged ? "remove-circle" : "add-circle"}
        label={`${staged ? "Unstage" : "Stage"} ${name}`}
        chrome={chrome}
        busy={busy}
        onPress={onToggleStaged}
        size={TouchTarget}
      />
    </View>
  );
});

const styles = StyleSheet.create({
  empty: { flex: 1, justifyContent: "center", paddingHorizontal: 16 },
  section: {
    height: SECTION_HEIGHT,
    flexDirection: "row",
    alignItems: "center",
    paddingLeft: 12,
    paddingRight: 8,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  sectionMain: { flex: 1, flexDirection: "row", alignItems: "center", gap: 6, height: SECTION_HEIGHT },
  sectionTitle: { ...TypeScale.label, fontWeight: "600", ...UiTextMetrics },
  sectionCount: { ...TypeScale.label, ...UiTextMetrics, fontVariant: ["tabular-nums"] },
  sectionDelta: { marginLeft: 6 },
  dir: { height: DIR_HEIGHT, flexDirection: "row", alignItems: "center", gap: 6, paddingRight: 12 },
  dirName: { flexShrink: 1, ...TypeScale.caption, ...UiTextMetrics },
  dirCount: { ...TypeScale.caption, ...UiTextMetrics, fontVariant: ["tabular-nums"] },
  row: { height: GIT_ROW_HEIGHT, flexDirection: "row", alignItems: "center" },
  rowMain: { flex: 1, minWidth: 0, height: GIT_ROW_HEIGHT, flexDirection: "row", alignItems: "center", gap: 8 },
  rowName: { flex: 1, minWidth: 0 },
  rowNameText: { ...TypeScale.compact, fontWeight: "500", ...UiTextMetrics },
  rowDir: { ...TypeScale.caption, ...UiTextMetrics },
});
