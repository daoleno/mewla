import React from "react";
import { ScrollView, StyleSheet } from "react-native";
import { EmptyState } from "../ui";
import { SealCat } from "./SealCat";

/**
 * Brain before it can chat: no computer paired (the seal is an empty bed),
 * connecting (one eye open) or offline (asleep in a greyed seal).
 */
export function BrainStatusState({
  hasServer,
  connected,
  animate = true,
  onSettings,
  onRetry,
}: {
  hasServer: boolean;
  connected: boolean;
  animate?: boolean;
  onSettings(): void;
  onRetry(): void;
}) {
  const state = !hasServer ? "homeless" : connected ? "waking" : "offline";
  return (
    <ScrollView contentContainerStyle={styles.center}>
      <EmptyState
        art={<SealCat state={state} size={120} animate={animate} />}
        title={
          state === "homeless"
            ? "Give Brain a home"
            : state === "waking"
              ? "Waking Brain"
              : "Brain is offline"
        }
        detail={
          state === "homeless"
            ? "Brain lives on your computer. Pair it and Brain moves in."
            : state === "waking"
              ? "Connecting to your computer."
              : "Your computer is unreachable. Brain picks up where it left off."
        }
        busy={state === "waking"}
        action={
          state === "homeless"
            ? { label: "Pair a computer", icon: "qr-code-outline", onPress: onSettings }
            : state === "offline"
              ? { label: "Retry connection", icon: "refresh-outline", onPress: onRetry }
              : undefined
        }
        secondary={
          hasServer
            ? { label: "Server settings", icon: "settings-outline", onPress: onSettings }
            : undefined
        }
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  center: {
    flexGrow: 1,
    justifyContent: "center",
  },
});
