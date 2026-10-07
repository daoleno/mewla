import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners, TypeScale } from "../../constants/tokens";
import { chromeTint } from "./composerMaterial";
import { ComposerLoadingDots } from "./ComposerLoadingDots";
import { useBrainCompanion } from "../mewla/BrainCompanion";
import { SealCat } from "../mewla/SealCat";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface InterfaceTimelineEmptyStateProps {
  chrome: TerminalThemeChrome;
  title: string;
  body?: string;
  busy?: boolean;
  /** Error states tint the glyph with the chrome's danger ink. */
  tone?: "default" | "error";
  icon?: IoniconName;
  actionLabel?: string;
  actionIcon?: IoniconName;
  onAction?: () => void;
  /** An empty or loading conversation: Brain shows its seal cat here. */
  conversationEmpty?: boolean;
}

/**
 * Chat-canvas counterpart of the app EmptyState: same halo, title, detail
 * and capsule action, drawn from the terminal-theme chrome so it follows the
 * Session's canvas instead of the app theme. In Brain the halo is the seal,
 * with the cat asleep in it (or peeking while the chat loads).
 */
export function InterfaceTimelineEmptyState({
  chrome,
  title,
  body,
  busy = false,
  tone = "default",
  icon = "chatbubble-ellipses-outline",
  actionLabel,
  actionIcon = "terminal-outline",
  onAction,
  conversationEmpty = false,
}: InterfaceTimelineEmptyStateProps) {
  const ink = tone === "error" ? chrome.danger : chrome.accent;
  const inkSoft = tone === "error" ? chrome.dangerSoft : chrome.accentSoft;
  const companion = useBrainCompanion();
  const seal = companion && conversationEmpty;
  return (
    <View style={styles.emptyState} accessibilityLiveRegion="polite">
      {seal ? (
        <SealCat
          state={busy ? "waking" : "idle"}
          size={SEAL_SIZE}
          animate={companion.animate}
          style={styles.seal}
        />
      ) : (
        <EmptyHalo ink={ink} inkSoft={inkSoft} busy={busy} icon={icon} />
      )}
      <Text
        style={[styles.emptyTitle, seal && styles.sealTitle, { color: chrome.text }]}
        accessibilityRole="header"
      >
        {title}
      </Text>
      {body ? (
        <Text style={[styles.emptyBody, { color: chrome.textMuted }]}>
          {body}
        </Text>
      ) : null}
      {actionLabel && onAction ? (
        <Pressable
          accessibilityLabel={actionLabel}
          accessibilityRole="button"
          onPress={() => {
            void Haptics.selectionAsync();
            onAction();
          }}
          style={({ pressed }) => [
            styles.emptyAction,
            {
              backgroundColor: chromeTint(chrome.accent, 0.14, chrome.accentSoft),
              opacity: pressed ? 0.75 : 1,
            },
          ]}
        >
          <Ionicons name={actionIcon} size={16} color={chrome.accent} />
          <Text style={[styles.emptyActionText, { color: chrome.accent }]}>
            {actionLabel}
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}

function EmptyHalo({
  ink,
  inkSoft,
  busy,
  icon,
}: {
  ink: string;
  inkSoft: string;
  busy: boolean;
  icon: IoniconName;
}) {
  return (
    <View
      accessible={false}
      style={[
        styles.halo,
        {
          backgroundColor: chromeTint(ink, 0.1, inkSoft),
          borderColor: chromeTint(ink, 0.18, "transparent"),
        },
      ]}
    >
      {busy ? (
        <ComposerLoadingDots color={ink} size={10} />
      ) : (
        <Ionicons name={icon} size={24} color={ink} />
      )}
    </View>
  );
}

const SEAL_SIZE = 112;

const styles = StyleSheet.create({
  // Calm, centered stack: a soft halo, a heading-weight title and one line
  // of guidance. Heading (not title) weight keeps an empty chat quiet.
  emptyState: {
    flexGrow: 1,
    minHeight: 240,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 36,
    paddingVertical: 24,
  },
  halo: {
    width: 56,
    height: 56,
    borderRadius: 28,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 14,
  },
  seal: {
    marginBottom: 22,
  },
  emptyTitle: {
    ...TypeScale.heading,
    textAlign: "center",
    maxWidth: 300,
  },
  sealTitle: {
    ...TypeScale.title,
  },
  emptyBody: {
    ...TypeScale.compact,
    marginTop: 6,
    textAlign: "center",
    maxWidth: 280,
  },
  emptyAction: {
    marginTop: 20,
    ...ContinuousCorners,
    minHeight: 44,
    borderRadius: 22,
    paddingHorizontal: 18,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  emptyActionText: {
    ...TypeScale.label,
  },
});
