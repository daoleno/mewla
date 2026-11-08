import React from "react";
import { StyleSheet, View } from "react-native";
import { EmptyState } from "../ui/EmptyState";

interface TerminalOutputStateCardProps {
  failed: boolean;
  busy: boolean;
  title: string;
  detail: string;
  hint: string;
  showRetry: boolean;
  onRetry(): void;
}

/** The terminal's waiting and unreachable states, in the shared EmptyState. */
export function TerminalOutputStateCard({
  failed,
  busy,
  title,
  detail,
  hint,
  showRetry,
  onRetry,
}: TerminalOutputStateCardProps) {
  return (
    <View style={styles.terminalState}>
      <EmptyState
        size="inline"
        busy={busy}
        tone={failed ? "danger" : "default"}
        title={title}
        detail={`${detail}\n${hint}`}
        action={showRetry ? { label: "Retry Connection", onPress: onRetry } : undefined}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  terminalState: { flex: 1, alignItems: "center", justifyContent: "center", paddingHorizontal: 18, paddingBottom: 32 },
});
