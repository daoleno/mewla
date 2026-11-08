import { DateStyle, formatDate } from "../../services/dateFormat";
import React, { useMemo } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useAppColors } from "../../constants/tokens";
import { codexStatusSections } from "../../services/codexStatus";
import type { CodexStatusState } from "./useCodexStatus";
import { AppText, BottomSheetFrame, EmptyState, IconButton, ListRow, ListSection } from "../ui";
import { Spinner } from "../ui/Spinner";

interface CodexStatusSheetProps {
  visible: boolean;
  state: CodexStatusState;
  onRetry(): void;
  onSwitchToTerminal?: () => void;
  onClose(): void;
}

/** Reads the terminal theme from the surrounding `TerminalThemeScope`. */
export function CodexStatusSheet({
  visible,
  state,
  onRetry,
  onSwitchToTerminal,
  onClose,
}: CodexStatusSheetProps) {
  const colors = useAppColors();
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
        <View style={styles.headerCopy}>
          <AppText variant="title">Status</AppText>
          <AppText variant="caption" tone="tertiary">
            {timestamp || "Codex"}
          </AppText>
        </View>
        {state.phase === "ready" ? (
          <IconButton icon="refresh" tone="input" size={40} accessibilityLabel="Refresh status" onPress={onRetry} />
        ) : null}
        <IconButton icon="close" tone="input" size={40} accessibilityLabel="Close status" onPress={onClose} />
      </View>

      {state.phase === "ready" ? (
        <ScrollView
          style={styles.content}
          contentContainerStyle={styles.contentBody}
          showsVerticalScrollIndicator={false}
        >
          {sections.length === 0 ? (
            <EmptyState size="inline" detail="Codex reported nothing for this chat yet." />
          ) : null}
          {sections.map((section) => (
            <ListSection key={section.key} title={section.title}>
              {[
                ...(section.meters ?? []).map((meter) => (
                  <ListRow
                    key={meter.key}
                    title={meter.label}
                    subtitle={`${Math.round(100 - meter.usedPercent)}% left${meter.resets ? ` · ${meter.resets}` : ""}`}
                    trailing={
                      <View style={[styles.meterTrack, { backgroundColor: colors.border }]}>
                        <View
                          style={[
                            styles.meterFill,
                            {
                              width: `${Math.max(0, 100 - meter.usedPercent)}%`,
                              backgroundColor: meter.usedPercent >= 90 ? colors.statusFailed : colors.accent,
                            },
                          ]}
                        />
                      </View>
                    }
                  />
                )),
                ...section.rows.map((row) => (
                  <ListRow key={row.key} title={row.label} subtitle={row.value} subtitleNumberOfLines={0} />
                )),
              ]}
            </ListSection>
          ))}
        </ScrollView>
      ) : state.phase === "failed" ? (
        <EmptyState
          size="inline"
          tone={state.unavailable ? "default" : "danger"}
          title={state.unavailable ? "No status for this chat" : "Couldn't load status"}
          detail={
            state.unavailable
              ? "Mewla reads status from the Codex it started. For this chat, type /status in the terminal."
              : state.message
          }
          action={state.unavailable ? undefined : { label: "Retry", icon: "refresh", onPress: onRetry }}
          secondary={onSwitchToTerminal ? { label: "Terminal", icon: "terminal", onPress: onSwitchToTerminal } : undefined}
          style={styles.state}
        />
      ) : (
        <View style={styles.loading}>
          <Spinner />
          <AppText variant="compact" tone="secondary">Asking Codex</AppText>
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
  return `Updated ${formatDate(date, DateStyle.time)}`;
}

const styles = StyleSheet.create({
  sheet: { paddingHorizontal: 14, paddingTop: 10, paddingBottom: 18 },
  header: { flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 4, paddingBottom: 12 },
  headerCopy: { flex: 1, minWidth: 0 },
  content: { maxHeight: 470 },
  contentBody: { paddingBottom: 8 },
  meterTrack: { width: 72, height: 6, borderRadius: 3, overflow: "hidden" },
  meterFill: { height: 6, borderRadius: 3 },
  state: { minHeight: 190 },
  loading: { minHeight: 190, alignItems: "center", justifyContent: "center", gap: 10 },
});
