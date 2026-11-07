import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { ContinuousCorners, Radii, TouchTarget, TypeScale } from "../../constants/tokens";
import type {
  GitDiffFileContentPayload,
  GitDiffFileInfo,
  GitDiffPatchPayload,
  GitDiffScope,
} from "../../services/gitDiff";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { DiffIconButton } from "./GitDiffReviewControls";
import { DiffViewerHost } from "./pierre/DiffViewerHost";
import { parseDiffViewerMessage, type DiffViewerHostHandle } from "./pierre/diffViewerProtocol";
import { useDiffViewerStyle } from "./pierre/useViewerStyle";
import { formatGitDiffBytes, gitDiffExpansionSides, planGitDiffReview } from "./gitDiffReview";

type ReaderState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready" }
  | { status: "empty" }
  | { status: "too-large"; bytes: number }
  | { status: "error"; message: string };

interface GitDiffReaderProps {
  /** Null keeps the reader warm and empty, so opening a file costs no cold start. */
  file: GitDiffFileInfo | null;
  scope: GitDiffScope;
  loadPatch(path: string): Promise<GitDiffPatchPayload>;
  loadFileContent(path: string): Promise<GitDiffFileContentPayload>;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  split: boolean;
  wrap: boolean;
  fontSize: number;
  refreshKey: number;
  showSearch: boolean;
  showHeaders: boolean;
  bottomInset: number;
  /** The find bar's own dismiss; its toggle lives in the header menu. */
  onCloseSearch?(): void;
}

const MAX_CACHED_PATCHES = 8;

export function GitDiffReader({
  file,
  scope,
  loadPatch,
  loadFileContent,
  chrome,
  theme,
  split,
  wrap,
  fontSize,
  refreshKey,
  showSearch,
  showHeaders,
  bottomInset,
  onCloseSearch,
}: GitDiffReaderProps) {
  const host = useRef<DiffViewerHostHandle>(null);
  const [state, setState] = useState<ReaderState>({ status: "idle" });
  const [attempt, setAttempt] = useState(0);
  const [draft, setDraft] = useState("");
  const [find, setFind] = useState({ query: "", count: 0, index: -1 });
  const patches = useRef(new Map<string, GitDiffPatchPayload>());
  const contents = useRef(new Map<string, Promise<GitDiffFileContentPayload>>());
  const activeKey = useRef("");
  const fileRef = useRef(file);
  fileRef.current = file;
  const path = file?.path ?? null;

  const layout = useMemo(
    () => ({ split, wrap, headers: showHeaders, inset: bottomInset + 12 }),
    [split, wrap, showHeaders, bottomInset],
  );
  useDiffViewerStyle(host, theme, chrome, fontSize, layout);

  useEffect(() => {
    const current = fileRef.current;
    if (!current || !path) {
      activeKey.current = "";
      host.current?.post([{ type: "clear" }]);
      setState({ status: "idle" });
      return;
    }
    let cancelled = false;
    const patchKey = `${refreshKey}:${path}`;
    setState({ status: "loading" });
    void (async () => {
      try {
        let payload = patches.current.get(patchKey);
        if (!payload) {
          payload = await loadPatch(path);
          if (patches.current.size >= MAX_CACHED_PATCHES) patches.current.clear();
          patches.current.set(patchKey, payload);
        }
        if (cancelled) return;
        const plan = planGitDiffReview(payload, fileRef.current ?? current, scope);
        if (plan.kind !== "sections") {
          activeKey.current = "";
          host.current?.post([{ type: "clear" }]);
          setState(plan.kind === "empty" ? { status: "empty" } : { status: "too-large", bytes: plan.bytes });
          return;
        }
        const key = `${patchKey}|${scope}`;
        activeKey.current = key;
        setFind({ query: "", count: 0, index: -1 });
        host.current?.post([{ type: "diff", key, sections: plan.sections }]);
      } catch (reason) {
        if (!cancelled) {
          setState({ status: "error", message: reason instanceof Error ? reason.message : "Could not load this diff." });
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [path, scope, refreshKey, loadPatch, attempt]);

  useEffect(() => {
    if (showSearch) return;
    setDraft("");
    setFind({ query: "", count: 0, index: -1 });
    host.current?.post([{ type: "find", query: "" }]);
  }, [showSearch]);

  const answerFiles = useCallback(
    async (id: string) => {
      const current = fileRef.current;
      const post = (oldContents: string | null, newContents: string | null, oldName?: string) =>
        host.current?.post([{ type: "files", id, oldContents, newContents, oldName }]);
      if (!current) return post(null, null);
      try {
        const key = `${refreshKey}:${current.path}`;
        let pending = contents.current.get(key);
        if (!pending) {
          pending = loadFileContent(current.path);
          contents.current.set(key, pending);
        }
        const sides = gitDiffExpansionSides(await pending, current);
        if (sides) post(sides.oldContents, sides.newContents, sides.oldName);
        else post(null, null);
      } catch {
        contents.current.delete(`${refreshKey}:${current.path}`);
        post(null, null);
      }
    },
    [loadFileContent, refreshKey],
  );

  const onMessage = useCallback(
    (raw: string) => {
      const message = parseDiffViewerMessage(raw);
      if (!message) return;
      switch (message.type) {
        case "loaded":
          if (message.key === activeKey.current) setState({ status: "ready" });
          break;
        case "needFiles":
          void answerFiles(message.id);
          break;
        case "find":
          setFind({ query: message.query, count: message.count, index: message.index });
          break;
        case "error":
          if (!message.key || message.key === activeKey.current) {
            setState({ status: "error", message: message.message });
          }
          break;
      }
    },
    [answerFiles],
  );

  const submitFind = () => host.current?.post([{ type: "find", query: draft.trim() }]);
  const seek = (direction: "next" | "previous") => host.current?.post([{ type: "seek", target: "match", direction }]);
  const seekDisabled = !find.query || find.count === 0;

  return (
    <View style={[styles.root, { backgroundColor: chrome.surface }]}>
      {showSearch ? (
        <View style={[styles.search, { borderColor: chrome.border, backgroundColor: chrome.appBackground }]}>
          <View style={[styles.field, { backgroundColor: chrome.surfaceMuted }]}>
            <MobileSingleLineInput
              accessibilityLabel="Search this diff"
              placeholder="Find in diff"
              value={draft}
              onChangeText={setDraft}
              onSubmitEditing={submitFind}
              returnKeyType="search"
              autoCapitalize="none"
              autoCorrect={false}
              autoFocus
              containerStyle={styles.input}
              inputStyle={{ color: chrome.text }}
              placeholderTextColor={chrome.textSubtle}
            />
            {find.query ? (
              <Text accessibilityLiveRegion="polite" style={[styles.matchCount, { color: chrome.textMuted }]}>
                {find.count === 0
                  ? "No matches"
                  : find.index < 0
                    ? `${find.count} ${find.count === 1 ? "match" : "matches"}`
                    : `${find.index + 1} of ${find.count}`}
              </Text>
            ) : null}
          </View>
          <DiffIconButton icon="chevron-up" label="Previous matching line" chrome={chrome} disabled={seekDisabled} onPress={() => seek("previous")} />
          <DiffIconButton icon="chevron-down" label="Next matching line" chrome={chrome} disabled={seekDisabled} onPress={() => seek("next")} />
          {onCloseSearch ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Done finding"
              onPress={onCloseSearch}
              style={({ pressed }) => [styles.done, { opacity: pressed ? 0.55 : 1 }]}
            >
              <Text style={[styles.doneText, { color: chrome.link }]}>Done</Text>
            </Pressable>
          ) : null}
        </View>
      ) : null}
      <View
        style={styles.body}
        accessibilityActions={[{ name: "increment", label: "Next hunk" }, { name: "decrement", label: "Previous hunk" }]}
        onAccessibilityAction={(event) =>
          host.current?.post([{ type: "seek", target: "hunk", direction: event.nativeEvent.actionName === "increment" ? "next" : "previous" }])
        }
      >
        <DiffViewerHost ref={host} onMessage={onMessage} backgroundColor={chrome.surface} accessibilityLabel="Diff" />
        {state.status !== "ready" && state.status !== "idle" ? (
          <View style={[styles.state, { backgroundColor: chrome.surface }]}>
            {state.status === "loading" ? (
              <EmptyState size="inline" busy title="Loading diff" />
            ) : state.status === "empty" ? (
              <EmptyState size="inline" title="No changes in this comparison" />
            ) : state.status === "too-large" ? (
              <EmptyState
                size="inline"
                icon="document-text"
                title="Diff too large to show"
                detail={formatGitDiffBytes(state.bytes)}
              />
            ) : (
              <InlineNotice
                tone="danger"
                title="Couldn't load this diff"
                detail={state.message}
                action={{ label: "Retry", onPress: () => setAttempt((value) => value + 1) }}
              />
            )}
          </View>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  body: { flex: 1 },
  search: { flexDirection: "row", alignItems: "center", gap: 2, paddingLeft: 12, paddingRight: 4, paddingVertical: 4, borderBottomWidth: StyleSheet.hairlineWidth },
  field: { flex: 1, minWidth: 0, minHeight: 38, flexDirection: "row", alignItems: "center", gap: 6, paddingRight: 10, borderRadius: Radii.sm, ...ContinuousCorners },
  input: { flex: 1, minWidth: 0 },
  matchCount: { ...TypeScale.caption, fontVariant: ["tabular-nums"] },
  done: { minHeight: TouchTarget, minWidth: TouchTarget, paddingHorizontal: 8, alignItems: "center", justifyContent: "center" },
  doneText: { ...TypeScale.label },
  state: { ...StyleSheet.absoluteFill, padding: 20, alignItems: "center", justifyContent: "center" },
});
