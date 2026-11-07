import React, { useCallback, useEffect, useMemo, useRef } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { gitDiffCounts, type GitDiffFileInfo, type GitDiffScope } from "../../services/gitDiff";
import { gitDiffTreeDecoration } from "./gitDiffReview";
import { TreeViewerHost } from "./pierre/TreeViewerHost";
import { parseTreeViewerMessage, type TreeViewerHostHandle } from "./pierre/treeViewerProtocol";
import { useTreeViewerStyle } from "./pierre/useViewerStyle";

/** Short lists need no search; it appears once the list outgrows a glance. */
const SEARCH_FROM_FILES = 6;

interface GitDiffFileTreeProps {
  files: GitDiffFileInfo[];
  scope: GitDiffScope;
  selectedPath: string | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  onSelectFile(path: string): void;
  onLongPressFile(file: GitDiffFileInfo): void;
}

/** The changed files as a tree: git status, `+N −M` counts and path search come from Trees. */
export function GitDiffFileTree({
  files,
  scope,
  selectedPath,
  theme,
  chrome,
  onSelectFile,
  onLongPressFile,
}: GitDiffFileTreeProps) {
  const host = useRef<TreeViewerHostHandle>(null);
  useTreeViewerStyle(host, theme, chrome);
  const byPath = useRef(new Map<string, GitDiffFileInfo>());
  byPath.current = useMemo(() => new Map(files.map((file) => [file.path, file])), [files]);
  const selectedRef = useRef(selectedPath);
  selectedRef.current = selectedPath;

  useEffect(() => {
    host.current?.post([
      {
        type: "paths",
        paths: files.map((file) => file.path),
        gitStatus: files.map((file) => ({ path: file.path, status: file.status })),
        decorations: Object.fromEntries(
          files.map((file) => [file.path, gitDiffTreeDecoration(file, gitDiffCounts(file, scope))]),
        ),
        expand: "open",
        search: files.length >= SEARCH_FROM_FILES,
        selected: selectedRef.current ?? undefined,
      },
    ]);
  }, [files, scope]);

  useEffect(() => {
    host.current?.post([{ type: "select", path: selectedPath }]);
  }, [selectedPath]);

  const onMessage = useCallback(
    (raw: string) => {
      const message = parseTreeViewerMessage(raw);
      if (!message) return;
      if (message.type === "open") onSelectFile(message.path);
      else if (message.type === "menu" && message.kind === "file") {
        const file = byPath.current.get(message.path);
        if (file) onLongPressFile(file);
      }
    },
    [onLongPressFile, onSelectFile],
  );

  return (
    <View style={styles.root}>
      <TreeViewerHost ref={host} onMessage={onMessage} backgroundColor={chrome.appBackground} accessibilityLabel="Changed files" />
    </View>
  );
}

const styles = StyleSheet.create({ root: { flex: 1 } });
