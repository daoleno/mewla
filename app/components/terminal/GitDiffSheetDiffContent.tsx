import React, { useEffect, useMemo, useState } from "react";
import { Pressable, StyleSheet, Switch, Text, View } from "react-native";
import * as Haptics from "expo-haptics";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { ContinuousCorners, Radii, TouchTarget, TypeScale, UiTextMetrics, useAppTheme } from "../../constants/tokens";
import {
  filterGitDiffFiles,
  type GitDiffFileContentPayload,
  type GitDiffFileInfo,
  type GitDiffPatchPayload,
  type GitDiffScope,
} from "../../services/gitDiff";
import { EmptyState } from "../ui/EmptyState";
import { GitDiffFileTree } from "./GitDiffFileTree";
import { GitDiffReader } from "./GitDiffReader";
import { DiffIconButton } from "./GitDiffReviewControls";
import { GitDiffDetailHeader } from "./GitDiffSheetTopChrome";
import { summarizeGitDiffFiles } from "./gitDiffPresentation";
import { withAlpha } from "./colorWithAlpha";

interface GitDiffSheetDiffContentProps {
  files: GitDiffFileInfo[];
  clean: boolean;
  branch?: string;
  repoTitle: string;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  loadPatch(path: string): Promise<GitDiffPatchPayload>;
  loadFileContent(path: string): Promise<GitDiffFileContentPayload>;
  refreshKey: number;
  scope: GitDiffScope;
  scopeCounts: Record<GitDiffScope, number>;
  selectedFile: GitDiffFileInfo | null;
  wide: boolean;
  bottomInset: number;
  diffSearchOpen: boolean;
  diffOptionsOpen: boolean;
  loading: boolean;
  onSelectFile(path: string): void;
  onScopeChange(scope: GitDiffScope): void;
  onClearSelection(): void;
  onToggleDiffSearch(): void;
  onToggleDiffOptions(): void;
  onBrowseFiles(): void;
  /** Opens the shared action menu for one file (row long-press, wide header). */
  onOpenFileActions(file: GitDiffFileInfo, reading: boolean): void;
}

const FONT_SIZES = { min: 10, max: 20, step: 2 };

export function GitDiffSheetDiffContent({
  files,
  clean,
  branch,
  repoTitle,
  theme,
  chrome,
  loadPatch,
  loadFileContent,
  refreshKey,
  scope,
  scopeCounts,
  selectedFile,
  wide,
  bottomInset,
  diffSearchOpen,
  diffOptionsOpen,
  loading,
  onSelectFile,
  onScopeChange,
  onClearSelection,
  onToggleDiffSearch,
  onToggleDiffOptions,
  onBrowseFiles,
  onOpenFileActions,
}: GitDiffSheetDiffContentProps) {
  const [wrap, setWrap] = useState(true);
  const [fontSize, setFontSize] = useState(12);
  const [showHeaders, setShowHeaders] = useState(false);
  const scopedFiles = useMemo(() => filterGitDiffFiles(files, scope), [files, scope]);

  const scopeTotal = scopeCounts[scope];
  const summary = summarizeGitDiffFiles(scopedFiles, scope);
  const noChanges = clean && files.length === 0;

  // The reader is one long-lived WebView: it stays mounted and empty under the
  // list, so opening a file never pays the runtime's cold start.
  const reader = (
    <>
      {selectedFile && diffOptionsOpen ? (
        <GitDiffOptionsStrip
          chrome={chrome}
          wrap={wrap}
          fontSize={fontSize}
          showHeaders={showHeaders}
          onWrapChange={setWrap}
          onFontSizeChange={setFontSize}
          onShowHeadersChange={setShowHeaders}
          onDone={onToggleDiffOptions}
        />
      ) : null}
      <GitDiffReader
        file={selectedFile}
        scope={scope}
        theme={theme}
        chrome={chrome}
        loadPatch={loadPatch}
        loadFileContent={loadFileContent}
        refreshKey={refreshKey}
        split={wide}
        wrap={wrap}
        fontSize={fontSize}
        showSearch={diffSearchOpen && Boolean(selectedFile)}
        showHeaders={showHeaders}
        bottomInset={bottomInset}
        onCloseSearch={onToggleDiffSearch}
      />
    </>
  );

  const emptyList = noChanges ? (
    <EmptyState
      icon="check-circle"
      title="Working tree clean"
      detail={branch ?? null}
      action={{ label: "Browse files", icon: "folder-open", onPress: onBrowseFiles }}
    />
  ) : (
    <EmptyState
      size="inline"
      title={scope === "staged" ? "Nothing staged" : scope === "working" ? "No working changes" : "No changes"}
    />
  );

  return (
    <View style={[styles.root, wide ? styles.rootWide : styles.rootStack]}>
      <View
        style={[
          styles.listPane,
          wide ? [styles.listPaneWide, { borderRightColor: chrome.border }] : styles.listPaneStack,
          { backgroundColor: chrome.appBackground },
        ]}
        importantForAccessibility={selectedFile && !wide ? "no-hide-descendants" : "auto"}
        accessibilityElementsHidden={Boolean(selectedFile && !wide)}
      >
        {noChanges ? null : (
          <View style={styles.listTop}>
            <GitDiffScopeControl chrome={chrome} scope={scope} counts={scopeCounts} onChange={onScopeChange} />
            {scopeTotal > 0 ? (
              <Text style={[styles.listMetaText, { color: chrome.textSubtle }]} numberOfLines={1} accessibilityLiveRegion="polite">
                {summary.label}
              </Text>
            ) : null}
          </View>
        )}
        {scopedFiles.length === 0 ? (
          <View style={styles.emptyList}>{emptyList}</View>
        ) : (
          <GitDiffFileTree
            files={scopedFiles}
            scope={scope}
            selectedPath={selectedFile?.path ?? null}
            theme={theme}
            chrome={chrome}
            onSelectFile={onSelectFile}
            onLongPressFile={(file) => onOpenFileActions(file, false)}
          />
        )}
      </View>

      {wide ? (
        <View style={[styles.detailPane, { backgroundColor: chrome.surface }]}>
          {selectedFile ? (
            <GitDiffDetailHeader
              chrome={chrome}
              theme={theme}
              file={selectedFile}
              scope={scope}
              repoTitle={repoTitle}
              loading={loading}
              onClear={onClearSelection}
              onOpenActions={() => onOpenFileActions(selectedFile, true)}
            />
          ) : null}
          {reader}
          {selectedFile ? null : (
            <View style={styles.idleDetail} pointerEvents="none">
              <EmptyState icon="git-compare" title="No file selected" style={styles.emptyDetail} />
            </View>
          )}
        </View>
      ) : (
        <View
          style={[styles.overlay, { backgroundColor: chrome.surface }, selectedFile ? null : styles.warm]}
          pointerEvents={selectedFile ? "auto" : "none"}
          importantForAccessibility={selectedFile ? "auto" : "no-hide-descendants"}
          accessibilityElementsHidden={!selectedFile}
        >
          {reader}
        </View>
      )}
    </View>
  );
}

function GitDiffScopeControl({
  chrome,
  scope,
  counts,
  onChange,
}: {
  chrome: TerminalThemeChrome;
  scope: GitDiffScope;
  counts: Record<GitDiffScope, number>;
  onChange(scope: GitDiffScope): void;
}) {
  const { colors, isLight } = useAppTheme();
  const options: { value: GitDiffScope; label: string }[] = [
    { value: "all", label: "All" },
    { value: "working", label: "Working" },
    { value: "staged", label: "Staged" },
  ];
  return (
    <View
      accessibilityRole="tablist"
      style={[styles.scopeTrack, { backgroundColor: withAlpha(chrome.text, isLight ? 0.06 : 0.08) }]}
    >
      {options.map((option) => {
        const active = scope === option.value;
        return (
          <Pressable
            key={option.value}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            accessibilityLabel={`${option.label}, ${counts[option.value]} files`}
            onPress={() => {
              if (active) return;
              void Haptics.selectionAsync();
              onChange(option.value);
            }}
            style={styles.scopeHit}
          >
            <View
              style={[
                styles.scope,
                active && [
                  styles.scopeActive,
                  { backgroundColor: isLight ? colors.bgSurface : colors.bgElevated, shadowColor: chrome.shadowColor },
                ],
              ]}
            >
              <Text
                style={[
                  styles.scopeLabel,
                  { color: active ? chrome.text : chrome.textMuted },
                ]}
                numberOfLines={1}
              >
                {option.label}
              </Text>
              <Text
                style={[styles.scopeCount, { color: active ? chrome.textMuted : chrome.textSubtle }]}
                numberOfLines={1}
              >
                {counts[option.value]}
              </Text>
            </View>
          </Pressable>
        );
      })}
    </View>
  );
}

function GitDiffOptionsStrip({
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
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Done with display options"
          onPress={onDone}
          style={({ pressed }) => [styles.textButton, { opacity: pressed ? 0.55 : 1 }]}
        >
          <Text style={[styles.textButtonLabel, { color: chrome.link }]}>Done</Text>
        </Pressable>
      </View>
      <View style={[styles.optionsCard, { backgroundColor: chrome.surface }]}>
        <View style={[styles.option, { borderBottomColor: chrome.border }]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>Wrap lines</Text>
          <Switch accessibilityLabel="Wrap lines" value={wrap} onValueChange={onWrapChange} />
        </View>
        <View style={[styles.option, { borderBottomColor: chrome.border }]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>Text size</Text>
          <View style={styles.stepper}>
            <DiffIconButton
              icon="remove"
              label="Smaller code text"
              chrome={chrome}
              disabled={fontSize <= FONT_SIZES.min}
              onPress={() => onFontSizeChange(fontSize - FONT_SIZES.step)}
            />
            <Text style={[styles.stepperValue, { color: chrome.text }]}>{fontSize}</Text>
            <DiffIconButton
              icon="add"
              label="Larger code text"
              chrome={chrome}
              disabled={fontSize >= FONT_SIZES.max}
              onPress={() => onFontSizeChange(fontSize + FONT_SIZES.step)}
            />
          </View>
        </View>
        <View style={[styles.option, styles.optionLast]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>File headers</Text>
          <Switch
            accessibilityLabel="File headers"
            value={showHeaders}
            onValueChange={onShowHeadersChange}
          />
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  rootStack: { flexDirection: "column" },
  rootWide: { flexDirection: "row" },
  listPane: { flex: 1, minWidth: 0 },
  listPaneStack: { flex: 1 },
  // An explicit basis: on web `flex: 0` alone resolves to a 0% basis and
  // collapses the master pane.
  listPaneWide: { flex: 0, flexBasis: 340, width: 340, borderRightWidth: StyleSheet.hairlineWidth },
  detailPane: { flex: 1, minWidth: 0 },
  overlay: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
  },
  // Mounted but unseen: full size, so the WebView lays out and loads.
  warm: { opacity: 0 },
  listTop: {
    paddingHorizontal: 16,
    paddingTop: 10,
    gap: 10,
  },
  emptyList: { flex: 1, justifyContent: "center", paddingHorizontal: 16 },
  scopeTrack: {
    flexDirection: "row",
    padding: 2,
    borderRadius: Radii.sm,
    ...ContinuousCorners,
  },
  scopeHit: {
    flex: 1,
    minHeight: TouchTarget - 4,
  },
  scope: {
    flex: 1,
    borderRadius: 10,
    ...ContinuousCorners,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  scopeActive: {
    shadowOpacity: 0.08,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
  },
  scopeLabel: {
    ...TypeScale.label,
    ...UiTextMetrics,
  },
  scopeCount: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    fontVariant: ["tabular-nums"],
  },
  textButton: {
    minHeight: TouchTarget,
    minWidth: TouchTarget,
    paddingHorizontal: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  textButtonLabel: {
    ...TypeScale.label,
    ...UiTextMetrics,
  },
  listMetaText: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    paddingBottom: 6,
    fontVariant: ["tabular-nums"],
  },
  idleDetail: { ...StyleSheet.absoluteFill, justifyContent: "center" },
  emptyDetail: { flex: 1, justifyContent: "center" },
  options: {
    paddingHorizontal: 16,
    paddingBottom: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  optionsHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingLeft: 16,
  },
  optionsTitle: {
    ...TypeScale.caption,
    ...UiTextMetrics,
  },
  optionsCard: {
    borderRadius: Radii.card,
    ...ContinuousCorners,
    paddingLeft: 16,
    overflow: "hidden",
  },
  option: {
    minHeight: TouchTarget + 4,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    paddingRight: 8,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  optionLast: { borderBottomWidth: 0 },
  optionLabel: { ...TypeScale.compact, ...UiTextMetrics },
  stepper: { flexDirection: "row", alignItems: "center" },
  stepperValue: {
    ...TypeScale.label,
    minWidth: 26,
    textAlign: "center",
    fontVariant: ["tabular-nums"],
  },
});
