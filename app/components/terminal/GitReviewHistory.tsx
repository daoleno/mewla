import React, { useCallback, useEffect, useRef, useState } from "react";
import { FlatList, Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { TypeScale, UiTextMetrics } from "../../constants/tokens";
import type { GitCommitDetail, GitCommitFile, GitCommitSummary } from "../../services/gitDiff";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { GIT_ROW_HEIGHT } from "./GitReviewList";
import { GitDelta, GitStatusMark } from "./GitReviewParts";
import { gitDiffStatusWord, gitDiffTone } from "./gitDiffPresentation";
import { splitGitReviewPath } from "./gitReviewModel";
import { formatGitAge } from "./gitReviewTime";
import type { GitReviewActions } from "./gitReviewActions";

const COMMIT_ROW_HEIGHT = 56;

/** The branch's recent commits, newest first; tap one for its files and diffs. */
export function GitReviewHistory({
  actions,
  refreshKey,
  chrome,
  bottomInset,
  onOpen,
}: {
  actions: GitReviewActions;
  refreshKey: number;
  chrome: TerminalThemeChrome;
  bottomInset: number;
  onOpen(commit: GitCommitSummary): void;
}) {
  const [commits, setCommits] = useState<GitCommitSummary[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const generation = useRef(0);

  const load = useCallback(
    async (skip: number) => {
      const mine = ++generation.current;
      setLoading(true);
      try {
        const page = await actions.log(skip);
        if (mine !== generation.current) return;
        setCommits((current) => (skip === 0 ? page.commits : [...current, ...page.commits]));
        setHasMore(page.has_more);
        setError(null);
      } catch (reason) {
        if (mine === generation.current) setError(reason instanceof Error ? reason.message : "Could not load history.");
      } finally {
        if (mine === generation.current) setLoading(false);
      }
    },
    [actions],
  );
  useEffect(() => {
    void load(0);
  }, [load, refreshKey]);

  if (error && !commits.length) {
    return (
      <View style={styles.state}>
        <EmptyState tone="danger" icon="warning-fill" title="Couldn't load history" detail={error} action={{ label: "Retry", icon: "refresh", onPress: () => void load(0), loading }} />
      </View>
    );
  }
  if (!commits.length) {
    return <View style={styles.state}>{loading ? <EmptyState busy title="Loading history" /> : <EmptyState size="inline" title="No commits yet" />}</View>;
  }
  return (
    <FlatList
      data={commits}
      keyExtractor={(commit) => commit.sha}
      getItemLayout={(_, index) => ({ length: COMMIT_ROW_HEIGHT, offset: COMMIT_ROW_HEIGHT * index, index })}
      onEndReachedThreshold={0.6}
      onEndReached={() => {
        if (hasMore && !loading) void load(commits.length);
      }}
      contentContainerStyle={{ paddingBottom: bottomInset + 8 }}
      ListFooterComponent={error ? <InlineNotice tone="danger" title="Couldn't load more" detail={error} style={styles.footer} /> : undefined}
      renderItem={({ item }) => <CommitRow commit={item} chrome={chrome} onPress={() => onOpen(item)} />}
    />
  );
}

function CommitRow({ commit, chrome, onPress }: { commit: GitCommitSummary; chrome: TerminalThemeChrome; onPress(): void }) {
  const refs = (commit.refs ?? []).slice(0, 2).join(" · ");
  const meta = [commit.short, commit.author, formatGitAge(commit.date)].join(" · ");
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${commit.subject}, ${commit.author}, ${formatGitAge(commit.date)} ago`}
      onPress={onPress}
      style={({ pressed }) => [styles.commit, { borderBottomColor: chrome.border, opacity: pressed ? 0.6 : 1 }]}
    >
      <Text numberOfLines={1} style={[styles.subject, { color: chrome.text }]}>
        {commit.subject || "(no message)"}
      </Text>
      <View style={styles.metaRow}>
        <Text numberOfLines={1} style={[styles.meta, { color: chrome.textSubtle }]}>
          {meta}
        </Text>
        {refs ? (
          <Text numberOfLines={1} style={[styles.refs, { color: chrome.link }]}>
            {refs}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

/** A commit's header and its changed files; the diffs open in the reader. */
export function GitReviewCommitFiles({
  commit,
  detail,
  loading,
  error,
  selectedPath,
  theme,
  chrome,
  bottomInset,
  onRetry,
  onOpen,
}: {
  commit: GitCommitSummary;
  detail: GitCommitDetail | null;
  loading: boolean;
  error: string | null;
  selectedPath: string | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  bottomInset: number;
  onRetry(): void;
  onOpen(file: GitCommitFile): void;
}) {
  if (error) {
    return (
      <View style={styles.state}>
        <EmptyState tone="danger" icon="warning-fill" title="Couldn't load this commit" detail={error} action={{ label: "Retry", icon: "refresh", onPress: onRetry, loading }} />
      </View>
    );
  }
  if (!detail) return <View style={styles.state}><EmptyState busy title="Loading commit" /></View>;
  return (
    <FlatList
      data={detail.files}
      keyExtractor={(file) => file.path}
      contentContainerStyle={{ paddingBottom: bottomInset + 8 }}
      ListHeaderComponent={
        <View style={[styles.detailHead, { borderBottomColor: chrome.border }]}>
          <Text style={[styles.detailSubject, { color: chrome.text }]}>{detail.subject || commit.subject}</Text>
          {detail.body ? (
            <Text numberOfLines={6} style={[styles.detailBody, { color: chrome.textMuted }]}>
              {detail.body.trim()}
            </Text>
          ) : null}
          <Text style={[styles.meta, { color: chrome.textSubtle }]}>
            {[commit.short, commit.author, formatGitAge(commit.date)].join(" · ")}
          </Text>
          {detail.truncated ? <Text style={[styles.meta, { color: chrome.danger }]}>Large commit: the last files are not shown.</Text> : null}
        </View>
      }
      renderItem={({ item }) => {
        const { directory, name } = splitGitReviewPath(item.path);
        return (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={`${name}, ${gitDiffStatusWord(gitDiffTone(item))}, ${item.additions} added, ${item.deletions} removed`}
            onPress={() => onOpen(item)}
            style={({ pressed }) => [
              styles.file,
              { backgroundColor: item.path === selectedPath ? chrome.surfaceActive : "transparent", opacity: pressed ? 0.6 : 1 },
            ]}
          >
            <GitStatusMark file={item} theme={theme} chrome={chrome} />
            <Text numberOfLines={1} style={styles.fileName}>
              <Text style={[styles.fileNameText, { color: chrome.text }]}>{name}</Text>
              {directory ? <Text style={[styles.fileDir, { color: chrome.textSubtle }]}>{`  ${directory}`}</Text> : null}
            </Text>
            <GitDelta additions={item.additions} deletions={item.deletions} binary={item.binary} theme={theme} chrome={chrome} />
          </Pressable>
        );
      }}
    />
  );
}

const styles = StyleSheet.create({
  state: { flex: 1, justifyContent: "center", paddingHorizontal: 16 },
  footer: { marginHorizontal: 16, marginTop: 8 },
  commit: { height: COMMIT_ROW_HEIGHT, justifyContent: "center", paddingHorizontal: 16, borderBottomWidth: StyleSheet.hairlineWidth },
  subject: { ...TypeScale.compact, fontWeight: "500", ...UiTextMetrics },
  metaRow: { flexDirection: "row", alignItems: "center", gap: 8 },
  meta: { flexShrink: 1, ...TypeScale.caption, ...UiTextMetrics },
  refs: { flexShrink: 0, maxWidth: "45%", ...TypeScale.caption, ...UiTextMetrics },
  detailHead: { paddingHorizontal: 16, paddingVertical: 10, gap: 4, borderBottomWidth: StyleSheet.hairlineWidth },
  detailSubject: { ...TypeScale.heading, fontSize: 15, lineHeight: 21, ...UiTextMetrics },
  detailBody: { ...TypeScale.caption, ...UiTextMetrics },
  file: { height: GIT_ROW_HEIGHT, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 12 },
  fileName: { flex: 1, minWidth: 0 },
  fileNameText: { ...TypeScale.compact, fontWeight: "500", ...UiTextMetrics },
  fileDir: { ...TypeScale.caption, ...UiTextMetrics },
});
