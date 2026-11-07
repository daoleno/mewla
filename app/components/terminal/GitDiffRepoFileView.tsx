import React, { useCallback, useEffect, useMemo, useRef } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import type { GitRepoFileContentPayload } from "../../services/gitDiff";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { GitDiffStateCard } from "./GitDiffStateCard";
import { DiffViewerHost } from "./pierre/DiffViewerHost";
import { parseDiffViewerMessage, type DiffViewerHostHandle } from "./pierre/diffViewerProtocol";
import { useDiffViewerStyle } from "./pierre/useViewerStyle";
import { formatGitDiffBytes } from "./gitDiffReview";

interface GitDiffRepoFileViewProps {
  path: string;
  payload?: GitRepoFileContentPayload;
  loading: boolean;
  error: string | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  bottomInset: number;
  onRetry?(): void;
}

const FILE_FONT_SIZE = 12;

/**
 * Working-tree file content only. The single sheet header owns the file title
 * and Back, so this view deliberately renders no second title bar.
 */
export function GitDiffRepoFileView({
  path,
  payload,
  loading,
  error,
  theme,
  chrome,
  bottomInset,
  onRetry,
}: GitDiffRepoFileViewProps) {
  const host = useRef<DiffViewerHostHandle>(null);
  const snapshot = payload?.snapshot ?? null;
  const showable = Boolean(snapshot?.exists && snapshot.content && !snapshot.binary);
  const layout = useMemo(
    () => ({ split: false, wrap: false, headers: false, inset: bottomInset + 12 }),
    [bottomInset],
  );
  useDiffViewerStyle(host, theme, chrome, FILE_FONT_SIZE, layout);

  useEffect(() => {
    if (showable && snapshot?.content !== undefined) {
      host.current?.post([{ type: "file", key: `file:${path}`, path, contents: snapshot.content }]);
    } else {
      host.current?.post([{ type: "clear" }]);
    }
  }, [path, showable, snapshot?.content]);

  // Nothing here answers the reader, but its errors must not pass silently.
  const onMessage = useCallback((raw: string) => {
    const message = parseDiffViewerMessage(raw);
    if (message?.type === "error") console.warn("[Git file view] " + message.message);
  }, []);

  return (
    <View style={styles.root}>
      <DiffViewerHost ref={host} onMessage={onMessage} backgroundColor={chrome.surface} accessibilityLabel="File" />
      {loading ? (
        <View style={[styles.state, { backgroundColor: chrome.surface, paddingBottom: bottomInset + 14 }]}>
          <EmptyState size="inline" busy title="Loading file" />
        </View>
      ) : error ? (
        <View style={[styles.state, { backgroundColor: chrome.surface, paddingBottom: bottomInset + 14 }]}>
          <InlineNotice
            tone="danger"
            title="Couldn't load file"
            detail={error}
            action={onRetry ? { label: "Retry", onPress: onRetry } : undefined}
          />
        </View>
      ) : !showable ? (
        <View style={[styles.state, { backgroundColor: chrome.surface, paddingBottom: bottomInset + 14 }]}>
          {snapshot?.binary ? (
            <GitDiffStateCard icon="cube" title="Binary file" accent={theme.cursor} chromeText={chrome.text} chromeMuted={chrome.textMuted} />
          ) : (
            <GitDiffStateCard
              icon="document-text"
              title={snapshot?.exists && !snapshot.content ? "Empty file" : "File snapshot unavailable"}
              detail={snapshot?.exists ? undefined : snapshot?.reason || "This file could not be read from the working tree."}
              accent={theme.cursor}
              chromeText={chrome.text}
              chromeMuted={chrome.textMuted}
            />
          )}
        </View>
      ) : snapshot?.truncated ? (
        <InlineNotice
          tone="warning"
          icon="cut"
          title="Showing part of this file"
          detail={`First ${formatGitDiffBytes(snapshot.content?.length ?? 0)} of ${formatGitDiffBytes(snapshot.byte_count)}.`}
          style={styles.truncation}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  state: { ...StyleSheet.absoluteFill, paddingHorizontal: 16, paddingVertical: 16, justifyContent: "flex-start" },
  truncation: { position: "absolute", left: 12, right: 12, bottom: 12 },
});
