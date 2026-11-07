import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import type {
  GitDiffFileInfo,
  GitRepoFileContentPayload,
  GitRepoPathsPayload,
} from "../../services/gitDiff";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { GitDiffRepoFileView } from "./GitDiffRepoFileView";
import { TreeViewerHost } from "./pierre/TreeViewerHost";
import { parseTreeViewerMessage, type TreeViewerHostHandle } from "./pierre/treeViewerProtocol";
import { useTreeViewerStyle } from "./pierre/useViewerStyle";

export interface GitRepoTreeEntry {
  path: string;
  kind: "file" | "directory";
}

interface GitDiffRepoBrowserProps {
  repoTitle: string;
  repoPaths: GitRepoPathsPayload | null;
  repoPathsLoading: boolean;
  repoPathsError: string | null;
  repoFilePath: string | null;
  repoFileContent?: GitRepoFileContentPayload;
  repoFileLoading: boolean;
  repoFileError: string | null;
  /** Changed files, so the tree can mark what the working tree touches. */
  changedFiles: GitDiffFileInfo[];
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  bottomInset: number;
  onRetryPaths(): void;
  onOpenRepoFile(path: string): void;
  onLongPressEntry?(entry: GitRepoTreeEntry): void;
}

/**
 * The working tree as one searchable tree. The sheet header is the only Back,
 * so this view draws no navigation of its own.
 */
export function GitDiffRepoBrowser({
  repoPaths,
  repoPathsLoading,
  repoPathsError,
  repoFilePath,
  repoFileContent,
  repoFileLoading,
  repoFileError,
  changedFiles,
  theme,
  chrome,
  bottomInset,
  onRetryPaths,
  onOpenRepoFile,
  onLongPressEntry,
}: GitDiffRepoBrowserProps) {
  // The file viewer is mounted by the first file opened and then kept, parked
  // under the tree, so every later file reuses its WebView and Back returns to
  // the tree as it was left.
  const [fileViewMounted, setFileViewMounted] = useState(Boolean(repoFilePath));
  useEffect(() => {
    if (repoFilePath) setFileViewMounted(true);
  }, [repoFilePath]);
  const fileOpen = Boolean(repoFilePath);
  const tree =
    repoPathsError && !repoPaths ? (
      <View style={styles.state}>
        <InlineNotice
          tone="danger"
          title="Couldn't load files"
          detail={repoPathsError}
          action={{ label: "Retry", onPress: onRetryPaths }}
        />
      </View>
    ) : !repoPaths ? (
      <View style={styles.state}>
        <EmptyState size="inline" busy={repoPathsLoading} title={repoPathsLoading ? "Loading files" : "No files"} />
      </View>
    ) : (
      <RepoTree
        repoPaths={repoPaths}
        changedFiles={changedFiles}
        theme={theme}
        chrome={chrome}
        onOpenRepoFile={onOpenRepoFile}
        onLongPressEntry={onLongPressEntry}
      />
    );
  return (
    <View style={styles.root}>
      {tree}
      {fileViewMounted ? (
        <View
          style={[StyleSheet.absoluteFill, fileOpen ? null : styles.parked]}
          pointerEvents={fileOpen ? "auto" : "none"}
          accessibilityElementsHidden={!fileOpen}
          importantForAccessibility={fileOpen ? "auto" : "no-hide-descendants"}
        >
          <GitDiffRepoFileView
            path={repoFilePath}
            payload={repoFileContent}
            loading={repoFileLoading}
            error={repoFileError}
            theme={theme}
            chrome={chrome}
            bottomInset={bottomInset}
            onRetry={() => repoFilePath && onOpenRepoFile(repoFilePath)}
          />
        </View>
      ) : null}
    </View>
  );
}

function RepoTree({
  repoPaths,
  changedFiles,
  theme,
  chrome,
  onOpenRepoFile,
  onLongPressEntry,
}: {
  repoPaths: GitRepoPathsPayload;
  changedFiles: GitDiffFileInfo[];
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  onOpenRepoFile(path: string): void;
  onLongPressEntry?(entry: GitRepoTreeEntry): void;
}) {
  const host = useRef<TreeViewerHostHandle>(null);
  useTreeViewerStyle(host, theme, chrome);
  const gitStatus = useMemo(
    () => changedFiles.map((file) => ({ path: file.path, status: file.status })),
    [changedFiles],
  );
  useEffect(() => {
    host.current?.post([
      { type: "paths", paths: repoPaths.paths, gitStatus, decorations: {}, expand: "closed", search: true },
    ]);
  }, [repoPaths, gitStatus]);
  const onMessage = useCallback(
    (raw: string) => {
      const message = parseTreeViewerMessage(raw);
      if (!message) return;
      if (message.type === "open") onOpenRepoFile(message.path);
      else if (message.type === "menu") onLongPressEntry?.({ path: message.path, kind: message.kind });
    },
    [onLongPressEntry, onOpenRepoFile],
  );
  return (
    <View style={styles.root}>
      <TreeViewerHost ref={host} onMessage={onMessage} backgroundColor={chrome.appBackground} accessibilityLabel="Repository files" />
      {repoPaths.truncated ? (
        <InlineNotice tone="warning" icon="cut" title="Showing part of this repository" style={styles.truncation} />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  state: { flex: 1, justifyContent: "center", paddingHorizontal: 16 },
  parked: { opacity: 0 },
  truncation: { position: "absolute", left: 12, right: 12, bottom: 12 },
});
