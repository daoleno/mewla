import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { ContinuousCorners, Radii, TouchTarget, TypeScale, UiTextMetrics } from "../../constants/tokens";
import type {
  GitCommitFile,
  GitDiffFileContentPayload,
  GitDiffPatchPayload,
} from "../../services/gitDiff";
import { ActionMenu, type ActionMenuItem } from "../ui/ActionMenu";
import { EmptyState } from "../ui/EmptyState";
import { InkSwitch } from "../ui/InkSwitch";
import { InlineNotice } from "../ui/InlineNotice";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { confirmDestructive } from "../ui/confirmDestructive";
import { useToast } from "../ui/Toast";
import { GitReviewActionBar } from "./GitReviewActionBar";
import { GitDelta, GitIconButton } from "./GitReviewParts";
import { GitAskAgentSheet } from "./GitReviewSheets";
import { buildGitDiffFileActions } from "./gitDiffActions";
import { formatGitDiffBytes, gitDiffExpansionSides, planGitDiffReview, canExpandGitDiffFile } from "./gitDiffReview";
import { formatGitAskAgentMessage } from "./gitAskAgent";
import {
  gitHunkAtLine,
  gitHunkSelection,
  gitSelectedLines,
  parseGitPatch,
  type GitParsedPatch,
  type GitPatchSide,
} from "./gitDiffHunks";
import { splitGitReviewPath, type GitReviewEntry } from "./gitReviewModel";
import type { GitReviewActions } from "./gitReviewActions";
import { DiffViewerHost } from "./pierre/DiffViewerHost";
import { parseDiffViewerMessage, type DiffViewerHostHandle, type DiffViewerSection } from "./pierre/diffViewerProtocol";
import { stepHunk } from "./pierre/runtime/diffNavigation";
import { useDiffViewerStyle } from "./pierre/useViewerStyle";

export type GitReviewSource =
  | { kind: "working"; entry: GitReviewEntry }
  | { kind: "commit"; sha: string; file: GitCommitFile; patch: string };

type ReaderState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready" }
  | { status: "empty" }
  | { status: "too-large"; bytes: number }
  | { status: "error"; message: string };

interface Cursor {
  /** The hunk the actions apply to, or null for the whole file. */
  hunk: number | null;
  selection: { side: GitPatchSide; start: number; end: number } | null;
}
const NO_CURSOR: Cursor = { hunk: null, selection: null };

interface GitReviewDiffProps {
  /** Null keeps the reader warm and empty, so opening a file costs no cold start. */
  source: GitReviewSource | null;
  /** Where this file sits among the changed files, for the next and previous controls. */
  position: { index: number; total: number } | null;
  loadPatch(path: string): Promise<GitDiffPatchPayload>;
  loadFileContent(path: string): Promise<GitDiffFileContentPayload>;
  refreshKey: number;
  actions: GitReviewActions;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  split: boolean;
  bottomInset: number;
  /** Shows a back control in the header (the narrow layout). */
  onBack?(): void;
  onNavigate(direction: "next" | "previous"): void;
  onStageFile(entry: GitReviewEntry): void;
  onDiscardFile(entry: GitReviewEntry): void;
  /** A hunk was discarded; `undo` puts it back (kept for the menu's Undo). */
  onHunkDiscarded(undo: () => Promise<void>): void;
  onOpenFile(path: string): void;
  onCopyPath(path: string): void;
  onCopyPatch(path: string, source: GitReviewSource): void;
  onRefresh(): void;
}

const FONT_SIZES = { min: 10, max: 20, step: 2 };
const MAX_CACHED_PATCHES = 8;

export function GitReviewDiff({
  source,
  position,
  loadPatch,
  loadFileContent,
  refreshKey,
  actions,
  theme,
  chrome,
  split,
  bottomInset,
  onBack,
  onNavigate,
  onStageFile,
  onDiscardFile,
  onHunkDiscarded,
  onOpenFile,
  onCopyPath,
  onCopyPatch,
  onRefresh,
}: GitReviewDiffProps) {
  const toast = useToast();
  const host = useRef<DiffViewerHostHandle>(null);
  const [state, setState] = useState<ReaderState>({ status: "idle" });
  const [attempt, setAttempt] = useState(0);
  const [wrap, setWrap] = useState(true);
  const [fontSize, setFontSize] = useState(12);
  const [showHeaders, setShowHeaders] = useState(false);
  const [optionsOpen, setOptionsOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [draft, setDraft] = useState("");
  const [find, setFind] = useState({ query: "", count: 0, index: -1 });
  const [section, setSection] = useState<DiffViewerSection | null>(null);
  const [cursor, setCursor] = useState<Cursor>(NO_CURSOR);
  const [busy, setBusy] = useState(false);
  const [askOpen, setAskOpen] = useState(false);
  const patches = useRef(new Map<string, GitDiffPatchPayload>());
  const contents = useRef(new Map<string, Promise<GitDiffFileContentPayload>>());
  const activeKey = useRef("");
  const sourceRef = useRef(source);
  sourceRef.current = source;

  const working = source?.kind === "working" ? source.entry : null;
  const path = source ? (source.kind === "working" ? source.entry.file.path : source.file.path) : null;
  const group = working?.group ?? null;
  // A stable key per comparison: the reader keeps its scroll position across
  // the reloads a stage or discard causes.
  const viewKey = source ? (source.kind === "working" ? `${source.entry.key}` : `${source.sha}:${source.file.path}`) : null;
  const commitPatch = source?.kind === "commit" ? source.patch : null;

  const layout = useMemo(() => ({ split, wrap, headers: showHeaders, inset: 12 }), [split, wrap, showHeaders]);
  useDiffViewerStyle(host, theme, chrome, fontSize, layout);

  const parsed = useMemo<GitParsedPatch | null>(() => {
    if (!section || section.id.endsWith("untracked")) return null;
    return parseGitPatch(section.patch);
  }, [section]);
  const hunkCount = parsed?.hunks.length ?? 0;

  const resetCursor = useCallback(() => {
    setCursor(NO_CURSOR);
    host.current?.post([{ type: "select", section: null, side: "additions", start: 0, end: 0, scroll: false }]);
  }, []);

  useEffect(() => {
    if (!source || !path || !viewKey) {
      activeKey.current = "";
      host.current?.post([{ type: "clear" }]);
      setState({ status: "idle" });
      setSection(null);
      setCursor(NO_CURSOR);
      return;
    }
    let cancelled = false;
    const reload = activeKey.current === viewKey;
    if (!reload) {
      setState({ status: "loading" });
      setCursor(NO_CURSOR);
    }
    const show = (sections: DiffViewerSection[]) => {
      activeKey.current = viewKey;
      setSection(sections[0] ?? null);
      setFind({ query: "", count: 0, index: -1 });
      setCursor(NO_CURSOR);
      host.current?.post([{ type: "diff", key: viewKey, sections }]);
    };
    const empty = (next: ReaderState) => {
      activeKey.current = "";
      setSection(null);
      host.current?.post([{ type: "clear" }]);
      setState(next);
    };
    void (async () => {
      try {
        if (source.kind === "commit") {
          if (!source.patch.trim()) return empty({ status: "empty" });
          show([{ id: "0-commit", patch: `${source.patch}\n`, expandable: false }]);
          return;
        }
        const cacheKey = `${refreshKey}:${path}`;
        let payload = patches.current.get(cacheKey);
        if (!payload) {
          payload = await loadPatch(path);
          if (patches.current.size >= MAX_CACHED_PATCHES) patches.current.clear();
          patches.current.set(cacheKey, payload);
        }
        if (cancelled) return;
        const plan = planGitDiffReview(payload, source.entry.file, source.entry.group);
        if (plan.kind === "empty") return empty({ status: "empty" });
        if (plan.kind === "too-large") return empty({ status: "too-large", bytes: plan.bytes });
        show(plan.sections);
      } catch (reason) {
        if (!cancelled) setState({ status: "error", message: reason instanceof Error ? reason.message : "Could not load this diff." });
      }
    })();
    return () => {
      cancelled = true;
    };
    // The comparison is named by viewKey; the refresh key and attempt reload it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [viewKey, commitPatch, refreshKey, loadPatch, attempt]);

  useEffect(() => {
    if (searchOpen) return;
    setDraft("");
    setFind({ query: "", count: 0, index: -1 });
    host.current?.post([{ type: "find", query: "" }]);
  }, [searchOpen]);

  const answerFiles = useCallback(
    async (id: string) => {
      const current = sourceRef.current;
      const post = (oldContents: string | null, newContents: string | null, oldName?: string) =>
        host.current?.post([{ type: "files", id, oldContents, newContents, oldName }]);
      if (current?.kind !== "working" || !canExpandGitDiffFile(current.entry.file)) return post(null, null);
      const file = current.entry.file;
      try {
        const key = `${refreshKey}:${file.path}`;
        let pending = contents.current.get(key);
        if (!pending) {
          pending = loadFileContent(file.path);
          contents.current.set(key, pending);
        }
        const sides = gitDiffExpansionSides(await pending, file);
        if (sides) post(sides.oldContents, sides.newContents, sides.oldName);
        else post(null, null);
      } catch {
        contents.current.delete(`${refreshKey}:${file.path}`);
        post(null, null);
      }
    },
    [loadFileContent, refreshKey],
  );

  const parsedRef = useRef(parsed);
  parsedRef.current = parsed;
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
        case "selection": {
          if (message.section === null) {
            setCursor(NO_CURSOR);
            break;
          }
          const hunk = parsedRef.current ? gitHunkAtLine(parsedRef.current, message.side, message.start) : null;
          setCursor({
            hunk: hunk ? hunk.index : null,
            selection: { side: message.side, start: message.start, end: message.end },
          });
          break;
        }
        case "error":
          if (!message.key || message.key === activeKey.current) setState({ status: "error", message: message.message });
          break;
      }
    },
    [answerFiles],
  );

  const stepHunks = useCallback(
    (direction: "next" | "previous") => {
      if (!parsed || !section || !parsed.hunks.length) return;
      const index = stepHunk(cursor.hunk ?? -1, parsed.hunks.length, direction);
      const range = gitHunkSelection(parsed.hunks[index]);
      if (!range) return;
      setCursor({ hunk: index, selection: range });
      host.current?.post([{ type: "select", section: section.id, ...range, scroll: true }]);
    },
    [cursor.hunk, parsed, section],
  );

  const hunk = cursor.hunk !== null && parsed ? parsed.hunks[cursor.hunk] ?? null : null;
  const scope = group === "staged" ? "staged" : "unstaged";
  const hunkable = Boolean(working && parsed && !working.file.untracked && !working.file.binary);

  const guarded = useCallback(
    async (run: () => Promise<void>, failure: string) => {
      setBusy(true);
      try {
        await run();
      } catch (reason) {
        toast.show({ tone: "error", title: failure, detail: reason instanceof Error ? reason.message : undefined });
      } finally {
        setBusy(false);
      }
    },
    [toast],
  );

  const onStageToggle = useCallback(() => {
    if (!working) return;
    if (hunk && hunkable) {
      void guarded(async () => {
        await actions.hunk(working.file.path, scope, group === "staged" ? "unstage" : "stage", hunk.text);
      }, group === "staged" ? "Couldn't unstage the hunk" : "Couldn't stage the hunk");
    } else {
      onStageFile(working);
    }
  }, [actions, group, guarded, hunk, hunkable, onStageFile, scope, working]);

  const onDiscard = useCallback(() => {
    if (!working) return;
    if (!(hunk && hunkable)) {
      onDiscardFile(working);
      return;
    }
    const text = hunk.text;
    const filePath = working.file.path;
    confirmDestructive({
      title: "Discard this hunk?",
      message: `Your edit to these lines in ${splitGitReviewPath(filePath).name} will be lost.`,
      confirmLabel: "Discard",
      onConfirm: () =>
        guarded(async () => {
          await actions.hunk(filePath, "unstaged", "discard", text);
          const undo = () => actions.hunk(filePath, "unstaged", "apply", text);
          onHunkDiscarded(undo);
          toast.show({
            title: "Hunk discarded",
            action: {
              label: "Undo",
              onPress: () => void undo().catch((reason) =>
                toast.show({ tone: "error", title: "Couldn't undo", detail: reason instanceof Error ? reason.message : undefined })),
            },
          });
        }, "Couldn't discard the hunk"),
    });
  }, [actions, guarded, hunk, hunkable, onDiscardFile, onHunkDiscarded, toast, working]);

  const askContext = useMemo(() => {
    if (!working) return null;
    const file = working.file.path;
    if (hunk && cursor.selection) {
      const { side, start, end } = cursor.selection;
      const lines = gitSelectedLines(hunk, side, start, end);
      const low = Math.min(start, end);
      const high = Math.max(start, end);
      return {
        where: `${file}:${low === high ? low : `${low}-${high}`}`,
        quote: lines.slice(0, 3).map((line) => `${line.kind === " " ? " " : line.kind} ${line.text}`),
        build: (comment: string) => formatGitAskAgentMessage({ path: file, side, start, end, lines, comment, staged: group === "staged" }),
      };
    }
    return {
      where: file,
      quote: [] as string[],
      build: (comment: string) => `${group === "staged" ? "In the staged diff" : "In the working-tree diff"}, ${file}:\n\n${comment.trim()}`,
    };
  }, [cursor.selection, group, hunk, working]);

  const menuItems = useMemo<ActionMenuItem[]>(() => {
    if (!source || !path) return [];
    return [
      ...buildGitDiffFileActions({
        path,
        deleted: source.kind === "working" ? source.entry.file.status === "deleted" : source.file.status === "deleted",
        reading: true,
        searchOpen,
        optionsOpen,
        onToggleSearch: () => setSearchOpen((value) => !value),
        onToggleOptions: () => setOptionsOpen((value) => !value),
        onOpenFile,
        onCopyPath,
        onCopyPatch: () => onCopyPatch(path, source),
      }),
      { key: "refresh", label: "Refresh", icon: "refresh", onPress: onRefresh },
    ];
  }, [onCopyPatch, onCopyPath, onOpenFile, onRefresh, optionsOpen, path, searchOpen, source]);

  const submitFind = () => host.current?.post([{ type: "find", query: draft.trim() }]);
  const seek = (direction: "next" | "previous") => host.current?.post([{ type: "seek", direction }]);
  const seekDisabled = !find.query || find.count === 0;
  const parts = path ? splitGitReviewPath(path) : null;
  const counts =
    source?.kind === "working"
      ? { additions: source.entry.additions, deletions: source.entry.deletions, binary: source.entry.file.binary }
      : source
        ? { additions: source.file.additions, deletions: source.file.deletions, binary: source.file.binary }
        : null;

  return (
    <View style={[styles.root, { backgroundColor: chrome.surface }]}>
      {source && parts ? (
        <View style={[styles.header, { borderBottomColor: chrome.border, backgroundColor: chrome.appBackground }]}>
          {onBack ? <GitIconButton icon="chevron-left" label="Back to changed files" chrome={chrome} onPress={onBack} /> : <View style={styles.headerPad} />}
          <View style={styles.headerCopy}>
            <Text numberOfLines={1} style={[styles.headerName, { color: chrome.text }]}>
              {parts.name}
            </Text>
            <View style={styles.headerMeta}>
              {counts ? <GitDelta additions={counts.additions} deletions={counts.deletions} binary={counts.binary} theme={theme} chrome={chrome} /> : null}
              {parts.directory ? (
                <Text numberOfLines={1} ellipsizeMode="head" style={[styles.headerDir, { color: chrome.textSubtle }]}>
                  {parts.directory}
                </Text>
              ) : null}
            </View>
          </View>
          {position ? (
            <>
              <GitIconButton icon="arrow-up" label="Previous file" chrome={chrome} size={40} disabled={position.index <= 0} onPress={() => onNavigate("previous")} />
              <GitIconButton icon="arrow-down" label="Next file" chrome={chrome} size={40} disabled={position.index >= position.total - 1} onPress={() => onNavigate("next")} />
            </>
          ) : null}
          <GitIconButton icon="more-vertical" label="More actions" chrome={chrome} size={40} onPress={() => setMenuVisible(true)} />
        </View>
      ) : null}
      {source && optionsOpen ? (
        <DisplayOptions
          chrome={chrome}
          wrap={wrap}
          fontSize={fontSize}
          showHeaders={showHeaders}
          onWrapChange={setWrap}
          onFontSizeChange={setFontSize}
          onShowHeadersChange={setShowHeaders}
          onDone={() => setOptionsOpen(false)}
        />
      ) : null}
      {source && searchOpen ? (
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
                {find.count === 0 ? "No matches" : find.index < 0 ? `${find.count}` : `${find.index + 1}/${find.count}`}
              </Text>
            ) : null}
          </View>
          <GitIconButton icon="chevron-up" label="Previous matching line" chrome={chrome} size={40} disabled={seekDisabled} onPress={() => seek("previous")} />
          <GitIconButton icon="chevron-down" label="Next matching line" chrome={chrome} size={40} disabled={seekDisabled} onPress={() => seek("next")} />
          <GitIconButton icon="close" label="Done finding" chrome={chrome} size={40} onPress={() => setSearchOpen(false)} />
        </View>
      ) : null}
      <View
        style={styles.body}
        accessibilityActions={[{ name: "increment", label: "Next hunk" }, { name: "decrement", label: "Previous hunk" }]}
        onAccessibilityAction={(event) => stepHunks(event.nativeEvent.actionName === "increment" ? "next" : "previous")}
      >
        <DiffViewerHost ref={host} onMessage={onMessage} backgroundColor={chrome.surface} accessibilityLabel="Diff" />
        {state.status !== "ready" && state.status !== "idle" ? (
          <View style={[styles.state, { backgroundColor: chrome.surface }]}>
            {state.status === "loading" ? (
              <EmptyState size="inline" busy title="Loading diff" />
            ) : state.status === "empty" ? (
              <EmptyState size="inline" title={source?.kind === "working" ? "Nothing left to review here" : "No changes"} />
            ) : state.status === "too-large" ? (
              <EmptyState size="inline" icon="document-text" title="Diff too large to show" detail={formatGitDiffBytes(state.bytes)} />
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
      {source && state.status === "ready" ? (
        <GitReviewActionBar
          chrome={chrome}
          group={group}
          hunks={{ count: hunkCount, index: hunkable || source.kind === "commit" ? cursor.hunk : null }}
          fileLabel={working?.file.binary ? "Binary" : working?.file.untracked ? "New file" : null}
          canAsk={Boolean(working && actions.askAgent)}
          busy={busy}
          bottomInset={bottomInset}
          onStep={stepHunks}
          onClearHunk={resetCursor}
          onStageToggle={onStageToggle}
          onDiscard={onDiscard}
          onAsk={() => setAskOpen(true)}
        />
      ) : null}
      <ActionMenu visible={menuVisible} title={path ?? undefined} items={menuItems} onClose={() => setMenuVisible(false)} />
      <GitAskAgentSheet
        visible={askOpen && Boolean(askContext)}
        where={askContext?.where ?? ""}
        quote={askContext?.quote ?? []}
        onClose={() => setAskOpen(false)}
        onSend={(comment) => {
          if (!askContext || !actions.askAgent) return;
          actions.askAgent(askContext.build(comment));
          setAskOpen(false);
          toast.show({ tone: "success", title: "Sent to the agent", detail: askContext.where });
        }}
      />
    </View>
  );
}

function DisplayOptions({
  chrome,
  wrap,
  fontSize,
  showHeaders,
  onWrapChange,
  onFontSizeChange,
  onShowHeadersChange,
  onDone,
}: {
  chrome: TerminalThemeChrome;
  wrap: boolean;
  fontSize: number;
  showHeaders: boolean;
  onWrapChange(value: boolean): void;
  onFontSizeChange(value: number): void;
  onShowHeadersChange(value: boolean): void;
  onDone(): void;
}) {
  return (
    <View style={[styles.options, { backgroundColor: chrome.appBackground, borderBottomColor: chrome.border }]}>
      <View style={styles.optionsHeader}>
        <Text accessibilityRole="header" style={[styles.optionsTitle, { color: chrome.textSubtle }]}>
          Display
        </Text>
        <Pressable accessibilityRole="button" accessibilityLabel="Done with display options" onPress={onDone} style={styles.done}>
          <Text style={[styles.doneText, { color: chrome.link }]}>Done</Text>
        </Pressable>
      </View>
      <View style={[styles.optionsCard, { backgroundColor: chrome.surface }]}>
        <View style={[styles.option, { borderBottomColor: chrome.border }]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>Wrap lines</Text>
          <InkSwitch accessibilityLabel="Wrap lines" value={wrap} onValueChange={onWrapChange} />
        </View>
        <View style={[styles.option, { borderBottomColor: chrome.border }]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>Text size</Text>
          <View style={styles.stepper}>
            <GitIconButton icon="remove" label="Smaller code text" chrome={chrome} size={40} disabled={fontSize <= FONT_SIZES.min} onPress={() => onFontSizeChange(fontSize - FONT_SIZES.step)} />
            <Text style={[styles.stepperValue, { color: chrome.text }]}>{fontSize}</Text>
            <GitIconButton icon="add" label="Larger code text" chrome={chrome} size={40} disabled={fontSize >= FONT_SIZES.max} onPress={() => onFontSizeChange(fontSize + FONT_SIZES.step)} />
          </View>
        </View>
        <View style={[styles.option, styles.optionLast]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>File headers</Text>
          <InkSwitch accessibilityLabel="File headers" value={showHeaders} onValueChange={onShowHeadersChange} />
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  body: { flex: 1 },
  header: { minHeight: 52, flexDirection: "row", alignItems: "center", borderBottomWidth: StyleSheet.hairlineWidth, paddingRight: 4 },
  headerPad: { width: 12 },
  headerCopy: { flex: 1, minWidth: 0 },
  headerName: { ...TypeScale.heading, fontSize: 15, lineHeight: 20, ...UiTextMetrics },
  headerMeta: { flexDirection: "row", alignItems: "center", gap: 8 },
  headerDir: { flexShrink: 1, ...TypeScale.caption, ...UiTextMetrics },
  search: { flexDirection: "row", alignItems: "center", gap: 2, paddingLeft: 12, paddingRight: 4, paddingVertical: 4, borderBottomWidth: StyleSheet.hairlineWidth },
  field: { flex: 1, minWidth: 0, minHeight: 38, flexDirection: "row", alignItems: "center", gap: 6, paddingRight: 10, borderRadius: Radii.sm, ...ContinuousCorners },
  input: { flex: 1, minWidth: 0 },
  matchCount: { ...TypeScale.caption, fontVariant: ["tabular-nums"] },
  state: { ...StyleSheet.absoluteFill, padding: 20, alignItems: "center", justifyContent: "center" },
  options: { paddingHorizontal: 16, paddingBottom: 12, borderBottomWidth: StyleSheet.hairlineWidth },
  optionsHeader: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingLeft: 16 },
  optionsTitle: { ...TypeScale.caption, ...UiTextMetrics },
  optionsCard: { borderRadius: Radii.card, ...ContinuousCorners, paddingLeft: 16, overflow: "hidden" },
  option: { minHeight: TouchTarget + 4, flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 12, paddingRight: 8, borderBottomWidth: StyleSheet.hairlineWidth },
  optionLast: { borderBottomWidth: 0 },
  optionLabel: { ...TypeScale.compact, ...UiTextMetrics },
  stepper: { flexDirection: "row", alignItems: "center" },
  stepperValue: { ...TypeScale.label, minWidth: 26, textAlign: "center", fontVariant: ["tabular-nums"] },
  done: { minHeight: TouchTarget, minWidth: TouchTarget, paddingHorizontal: 8, alignItems: "center", justifyContent: "center" },
  doneText: { ...TypeScale.label, ...UiTextMetrics },
});
