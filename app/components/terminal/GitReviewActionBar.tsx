import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale, UiTextMetrics } from "../../constants/tokens";
import type { GitReviewGroup } from "./gitReviewModel";
import { GitIconButton, GitTextButton } from "./GitReviewParts";

export interface GitReviewBarHunks {
  count: number;
  /** The hunk the actions apply to; null means the whole file. */
  index: number | null;
}

/**
 * The reader's one action row. With a hunk selected (tap a line, or step with
 * the arrows) the actions apply to that hunk; with none they apply to the
 * whole file. Staged diffs offer Unstage; changes offer Stage and Discard.
 */
export function GitReviewActionBar({
  chrome,
  group,
  hunks,
  fileLabel,
  canAsk,
  busy,
  bottomInset,
  onStep,
  onClearHunk,
  onStageToggle,
  onDiscard,
  onAsk,
}: {
  chrome: TerminalThemeChrome;
  /** Null in a commit's diff, where only reading tools apply. */
  group: GitReviewGroup | null;
  hunks: GitReviewBarHunks;
  /** What a file-level action is on a hunkless diff: "New file", "Binary". */
  fileLabel: string | null;
  canAsk: boolean;
  busy: boolean;
  bottomInset: number;
  onStep(direction: "next" | "previous"): void;
  onClearHunk(): void;
  onStageToggle(): void;
  onDiscard(): void;
  onAsk(): void;
}) {
  const hunkMode = hunks.index !== null;
  const unit = hunkMode ? "hunk" : "file";
  const label = hunkMode
    ? `${(hunks.index ?? 0) + 1}/${hunks.count}`
    : fileLabel ?? (hunks.count ? `${hunks.count} ${hunks.count === 1 ? "hunk" : "hunks"}` : "");
  return (
    <View style={[styles.bar, { backgroundColor: chrome.appBackground, borderTopColor: chrome.border, paddingBottom: bottomInset }]}>
      <View style={styles.cluster}>
        {hunkMode ? <GitIconButton icon="close" label="Select the whole file" chrome={chrome} size={36} onPress={onClearHunk} /> : null}
        {hunks.count > 0 ? (
          <>
            <GitIconButton icon="chevron-up" label="Previous hunk" chrome={chrome} size={40} onPress={() => onStep("previous")} />
            <Text accessibilityLiveRegion="polite" style={[styles.label, { color: chrome.textMuted }]}>
              {label}
            </Text>
            <GitIconButton icon="chevron-down" label="Next hunk" chrome={chrome} size={40} onPress={() => onStep("next")} />
          </>
        ) : label ? (
          <Text style={[styles.label, { color: chrome.textMuted }]}>{label}</Text>
        ) : null}
      </View>
      <View style={styles.spacer} />
      {canAsk ? <GitIconButton icon="chat" label={`Ask the agent about this ${unit}`} chrome={chrome} onPress={onAsk} /> : null}
      {group === "changes" ? (
        <GitIconButton icon="trash" label={`Discard ${unit}`} chrome={chrome} color={chrome.danger} disabled={busy} onPress={onDiscard} />
      ) : null}
      {group ? (
        <GitTextButton
          label={`${group === "staged" ? "Unstage" : "Stage"} ${unit}`}
          tone={group === "staged" ? "tinted" : "primary"}
          chrome={chrome}
          busy={busy}
          onPress={onStageToggle}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    minHeight: 52,
    flexDirection: "row",
    alignItems: "center",
    paddingLeft: 6,
    paddingRight: 12,
    gap: 2,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  cluster: { flexDirection: "row", alignItems: "center" },
  spacer: { flex: 1 },
  label: { ...TypeScale.label, ...UiTextMetrics, minWidth: 34, textAlign: "center", fontVariant: ["tabular-nums"] },
});
