import React, { useMemo } from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ContinuousCorners, Typography } from "../../constants/tokens";
import { codexStatusSections } from "../../services/codexStatus";
import type { CodexStatusState } from "./useCodexStatus";
import { BottomSheetFrame } from "../ui";
import { ComposerLoadingDots } from "./ComposerLoadingDots";
import { OutlineWidth, outlinedSurface } from "../ui/outlinedSurface";
import { Icon } from "../icons/Icon";

interface CodexStatusSheetProps {
  visible: boolean;
  state: CodexStatusState;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onRetry(): void;
  onSwitchToTerminal?: () => void;
  onClose(): void;
}

export function CodexStatusSheet({
  visible,
  state,
  chrome,
  theme,
  onRetry,
  onSwitchToTerminal,
  onClose,
}: CodexStatusSheetProps) {
  const sections = useMemo(
    () => (state.phase === "ready" ? codexStatusSections(state.status) : []),
    [state],
  );
  const timestamp =
    state.phase === "ready" ? formatStatusTimestamp(state.fetchedAt) : undefined;

  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="76%"
      cardStyle={styles.sheet}
      onClose={onClose}
    >
      <View style={styles.header}>
        <View
          style={[
            styles.headerIcon,
            { backgroundColor: chrome.accentSoft },
          ]}
        >
          <Icon name="pulse" size={18} color={chrome.accent} />
        </View>
        <View style={styles.headerCopy}>
          <Text style={[styles.title, { color: chrome.text }]}>Status</Text>
          <Text style={[styles.subtitle, { color: chrome.textSubtle }]}>
            {timestamp || "Codex"}
          </Text>
        </View>
        {state.phase === "ready" ? (
          <TouchableOpacity
            accessibilityLabel="Refresh status"
            accessibilityRole="button"
            style={[styles.closeButton, { backgroundColor: chrome.surfaceMuted }]}
            onPress={onRetry}
            activeOpacity={0.78}
          >
            <Icon name="refresh" size={18} color={chrome.textSubtle} />
          </TouchableOpacity>
        ) : null}
        <TouchableOpacity
          accessibilityLabel="Close status"
          accessibilityRole="button"
          style={[styles.closeButton, { backgroundColor: chrome.surfaceMuted }]}
          onPress={onClose}
          activeOpacity={0.78}
        >
          <Icon name="close" size={18} color={chrome.textSubtle} />
        </TouchableOpacity>
      </View>

      {state.phase === "ready" ? (
        <ScrollView
          style={styles.content}
          contentContainerStyle={styles.contentBody}
          showsVerticalScrollIndicator={false}
        >
          {sections.length === 0 ? (
            <Text style={[styles.stateText, { color: chrome.textSubtle }]}>
              Codex reported nothing for this chat yet.
            </Text>
          ) : null}
          {sections.map((section) => (
            <View key={section.key} style={styles.rawSection}>
              <Text style={[styles.sectionLabel, { color: chrome.textSubtle }]}>
                {section.title}
              </Text>
              <View
                style={[
                  styles.rows,
                  { borderColor: chrome.border, backgroundColor: chrome.surfaceMuted },
                ]}
              >
                {section.meters?.map((meter, index) => (
                  <View
                    key={meter.key}
                    style={[
                      styles.row,
                      index > 0
                        ? {
                            borderTopColor: chrome.border,
                            borderTopWidth: StyleSheet.hairlineWidth,
                          }
                        : null,
                    ]}
                  >
                    <Text
                      style={[styles.rowKey, { color: chrome.textSubtle }]}
                      numberOfLines={1}
                    >
                      {meter.label}
                    </Text>
                    <View
                      style={[styles.meterTrack, { backgroundColor: chrome.border }]}
                    >
                      <View
                        style={[
                          styles.meterFill,
                          {
                            width: `${Math.max(0, 100 - meter.usedPercent)}%`,
                            backgroundColor:
                              meter.usedPercent >= 90 ? theme.red : chrome.accent,
                          },
                        ]}
                      />
                    </View>
                    <Text style={[styles.rowValue, { color: chrome.text }]}>
                      {`${Math.round(100 - meter.usedPercent)}% left${meter.resets ? ` · ${meter.resets}` : ""}`}
                    </Text>
                  </View>
                ))}
                {section.rows.map((row, index) => (
                  <View
                    key={row.key}
                    style={[
                      styles.row,
                      index > 0 || section.meters?.length
                        ? {
                            borderTopColor: chrome.border,
                            borderTopWidth: StyleSheet.hairlineWidth,
                          }
                        : null,
                    ]}
                  >
                    <Text
                      style={[styles.rowKey, { color: chrome.textSubtle }]}
                      numberOfLines={1}
                    >
                      {row.label}
                    </Text>
                    <Text
                      style={[styles.rowValue, { color: chrome.text }]}
                      selectable
                    >
                      {row.value}
                    </Text>
                  </View>
                ))}
              </View>
            </View>
          ))}
        </ScrollView>
      ) : state.phase === "failed" ? (
        <View style={styles.state}>
          <Icon name="warning-fill" size={20} color={theme.yellow} />
          <Text style={[styles.stateTitle, { color: chrome.text }]}>
            {state.unavailable
              ? "No status for this chat"
              : "Couldn't load status"}
          </Text>
          <Text style={[styles.stateText, { color: chrome.textSubtle }]}>
            {state.unavailable
              ? "Mewla reads status from the Codex it started. For this chat, type /status in the terminal."
              : state.message}
          </Text>
          <View style={styles.actions}>
            {state.unavailable ? null : (
              <TouchableOpacity
                accessibilityLabel="Retry status"
                accessibilityRole="button"
                style={[styles.actionButton, { backgroundColor: chrome.accent }]}
                onPress={onRetry}
                activeOpacity={0.82}
              >
                <Icon name="refresh" size={15} color={theme.cursorAccent} />
                <Text style={[styles.primaryActionText, { color: theme.cursorAccent }]}>
                  Retry
                </Text>
              </TouchableOpacity>
            )}
            {onSwitchToTerminal ? (
              <TouchableOpacity
                accessibilityLabel="Open terminal"
                accessibilityRole="button"
                style={[
                  styles.actionButton,
                  styles.secondaryActionButton,
                  { borderColor: chrome.border, backgroundColor: chrome.surfaceMuted },
                ]}
                onPress={onSwitchToTerminal}
                activeOpacity={0.78}
              >
                <Icon name="terminal" size={15} color={chrome.textSubtle} />
                <Text style={[styles.secondaryActionText, { color: chrome.text }]}>
                  Terminal
                </Text>
              </TouchableOpacity>
            ) : null}
          </View>
        </View>
      ) : (
        <View style={styles.state}>
          <ComposerLoadingDots color={chrome.accent} size={10} />
          <Text style={[styles.stateTitle, { color: chrome.text }]}>
            Asking Codex
          </Text>
        </View>
      )}
    </BottomSheetFrame>
  );
}

function formatStatusTimestamp(value?: string) {
  if (!value) {
    return undefined;
  }
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) {
    return undefined;
  }
  return `Updated ${date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
}

const styles = StyleSheet.create({
  sheet: {
    paddingHorizontal: 14,
    paddingTop: 10,
    paddingBottom: 18,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 4,
    paddingBottom: 12,
  },
  headerIcon: {
    width: 34,
    height: 34,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 17,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    marginTop: 1,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  closeButton: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  content: {
    maxHeight: 470,
  },
  contentBody: {
    paddingBottom: 8,
  },
  rows: {
    ...outlinedSurface(8),
    overflow: "hidden",
  },
  row: {
    minHeight: 42,
    paddingHorizontal: 10,
    paddingVertical: 8,
    gap: 3,
  },
  rowKey: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
  },
  rowValue: {
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
  },
  rawSection: {
    marginTop: 12,
    gap: 6,
  },
  sectionLabel: {
    paddingHorizontal: 2,
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
  },
  meterTrack: {
    height: 6,
    borderRadius: 3,
    overflow: "hidden",
  },
  meterFill: {
    height: 6,
    borderRadius: 3,
  },
  state: {
    minHeight: 190,
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
    paddingHorizontal: 20,
  },
  stateTitle: {
    fontSize: 14,
    lineHeight: 18,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  stateText: {
    fontSize: 12,
    lineHeight: 17,
    textAlign: "center",
    fontFamily: Typography.uiFont,
  },
  actions: {
    marginTop: 8,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  actionButton: {
    minHeight: 44,
    borderRadius: 8,
    ...ContinuousCorners,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  secondaryActionButton: {
    borderWidth: OutlineWidth,
  },
  primaryActionText: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  secondaryActionText: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
});
