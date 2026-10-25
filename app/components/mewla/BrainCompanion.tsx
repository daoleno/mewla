import React, { createContext, useCallback, useContext, useEffect, useState } from "react";
import { AccessibilityInfo, Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import type { BrainCatPresence } from "./brainCatState";
import { TappableCat } from "./TappableCat";
import type { BrainWorkSlip } from "../brain/brainWorkSurface";
import type { WorkSlipAction } from "../brain/WorkSlip";

export interface BrainCompanion {
  presence: BrainCatPresence;
  /** False while the Brain screen is hidden, so the cat stops moving. */
  animate: boolean;
  /** Live Session id → "Claude Code · perpetuo", for the slips' meta line. */
  sessionLabels?: ReadonlyMap<string, string>;
  /** Shows the Work list (the sheet, or the column beside the chat); the tail row hands off to it. */
  onOpenWork?: () => void;
  /** The live slip for a Work, with its inline actions, while it is current. */
  workSlip?: (workId: string) => { slip: BrainWorkSlip; actions: readonly WorkSlipAction[] } | undefined;
  /** The Working row reports while it is on screen, so only one cat shows. */
  onTurnRunning?: (running: boolean) => void;
  /**
   * Tapping the cat. It returns the line the cat says back, if any (the
   * screen may also act: open a slip, retry the connection).
   */
  onCatTap?: (context: { turnRunning: boolean; turnLabel?: string }) => string | null | undefined;
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
  turnRunning = false,
}: {
  companion: BrainCompanion;
  label: string;
  detail?: string;
  chrome: TerminalThemeChrome;
  /** Between turns the row's text shows the Work list. */
  onPress?: () => void;
  /** This is the Working row of a running turn. */
  turnRunning?: boolean;
}) {
  const state = companion.presence.state;
  const { said, tap } = useCatSays(companion, turnRunning, detail || label);
  const onTurnRunning = companion.onTurnRunning;
  useEffect(() => {
    if (!turnRunning || !onTurnRunning) return;
    onTurnRunning(true);
    return () => onTurnRunning(false);
  }, [onTurnRunning, turnRunning]);
  return (
    <View style={styles.row}>
      <TappableCat
        state={state}
        size={ROW_CAT}
        animate={companion.animate}
        onPress={companion.onCatTap ? tap : undefined}
        accessibilityLabel={`Brain: ${label}`}
        accessibilityHint={turnRunning ? "Says what Brain is doing" : "Brain answers"}
      />
      <Pressable
        disabled={!onPress}
        onPress={onPress}
        style={({ pressed }) => [styles.copy, pressed ? styles.pressed : null]}
        accessible
        accessibilityRole={onPress ? "button" : "text"}
        accessibilityLabel={detail ? `Brain: ${label}, ${detail}` : `Brain: ${label}`}
        accessibilityHint={onPress ? "Shows the Work list" : undefined}
        accessibilityLiveRegion="polite"
      >
        <Text style={[styles.label, { color: chrome.text }]} numberOfLines={1}>
          {said ?? label}
        </Text>
        {detail && !said ? (
          <Text style={[styles.detail, { color: chrome.textMuted }]} numberOfLines={1}>
            {detail}
          </Text>
        ) : null}
      </Pressable>
    </View>
  );
}

/** How long the cat's answer stays before the row reads as before. */
const CAT_SAYS_MS = 4500;

/** The cat's answer to a tap, shown in place of the row's text for a moment. */
export function useCatSays(companion: BrainCompanion | null, turnRunning: boolean, turnLabel?: string) {
  const [said, setSaid] = useState<string | null>(null);
  useEffect(() => {
    if (!said) return;
    const timer = setTimeout(() => setSaid(null), CAT_SAYS_MS);
    return () => clearTimeout(timer);
  }, [said]);
  const tap = useCallback(() => {
    const line = companion?.onCatTap?.({ turnRunning, turnLabel });
    if (line) {
      setSaid(line);
      AccessibilityInfo.announceForAccessibility(line);
    }
  }, [companion, turnLabel, turnRunning]);
  return { said, tap };
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
