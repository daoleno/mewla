import React, { createContext, useContext } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import type { BrainCatPresence } from "./brainCatState";
import { SealCat } from "./SealCat";

export interface BrainCompanion {
  presence: BrainCatPresence;
  /** False while the Brain screen is hidden, so the cat stops moving. */
  animate: boolean;
  /** Live Session id → "Claude Code · perpetuo", for the slips' meta line. */
  sessionLabels?: ReadonlyMap<string, string>;
  /** Opens the Work list; the tail row hands off to it. */
  onOpenWork?: () => void;
}

/**
 * Brain-only opt-in. The Brain screen provides it; Session chats never do,
 * so the shared chat timeline shows the cat only for Brain.
 */
export const BrainCompanionContext = createContext<BrainCompanion | null>(null);

export function useBrainCompanion(): BrainCompanion | null {
  return useContext(BrainCompanionContext);
}

/** Timeline id prefix for the tail row the cat holds between turns. */
export { BRAIN_PRESENCE_ITEM_PREFIX } from "./brainPresenceTimeline";

const ROW_CAT = 46;

/**
 * The cat at the newest edge of the Brain timeline: on its feet while Brain
 * works, or announcing what is waiting between turns.
 */
export function BrainCatRow({
  companion,
  label,
  detail,
  chrome,
  onPress,
}: {
  companion: BrainCompanion;
  label: string;
  detail?: string;
  chrome: TerminalThemeChrome;
  /** Between turns the row opens the Work list. */
  onPress?: () => void;
}) {
  return (
    <Pressable
      disabled={!onPress}
      onPress={onPress}
      style={({ pressed }) => [styles.row, pressed ? styles.pressed : null]}
      accessible
      accessibilityRole={onPress ? "button" : "text"}
      accessibilityLabel={detail ? `Brain: ${label}, ${detail}` : `Brain: ${label}`}
      accessibilityHint={onPress ? "Opens the Work list" : undefined}
      accessibilityLiveRegion="polite"
    >
      <SealCat state={companion.presence.state} size={ROW_CAT} animate={companion.animate} />
      <View style={styles.copy}>
        <Text style={[styles.label, { color: chrome.text }]} numberOfLines={1}>
          {label}
        </Text>
        {detail ? (
          <Text style={[styles.detail, { color: chrome.textMuted }]} numberOfLines={1}>
            {detail}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 10,
    paddingVertical: 6,
  },
  pressed: {
    opacity: 0.72,
  },
  copy: {
    flexShrink: 1,
    paddingBottom: 4,
  },
  label: {
    ...TypeScale.label,
  },
  detail: {
    ...TypeScale.caption,
  },
});
