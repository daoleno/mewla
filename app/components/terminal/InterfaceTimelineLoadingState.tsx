import React from "react";
import { StyleSheet, Text, View } from "react-native";
import { useReducedMotion } from "react-native-reanimated";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import { useBrainCompanion, useCatSays, type BrainCompanion } from "../mewla/BrainCompanion";
import { TappableCat } from "../mewla/TappableCat";

/** The empty state's seal size, so the cat wakes where it would sleep. */
const CAT_SIZE = 112;

const WAKING: BrainCompanion["presence"] = { state: "waking" };

/**
 * Brain's conversation while its history loads: the chosen pet hops out of
 * its seal and stands, with one line. No outline of messages, and never the
 * empty state's words, because history may be on its way. The timeline only
 * mounts this once a load has run past its delay (useLoadingVeil), so the
 * hop starts from the seal. When the Work column holds the cat, the chat
 * shows none, so there is one cat on screen.
 */
export function InterfaceTimelineLoadingState({
  chrome,
}: {
  chrome: TerminalThemeChrome;
}) {
  const companion = useBrainCompanion();
  const reduced = useReducedMotion();
  const waking = companion && !companion.presence.away ? { ...companion, presence: WAKING } : null;
  const { said, tap } = useCatSays(
    waking ? { ...waking, onCatTap: () => "Still waking up…" } : null,
    false,
  );
  return (
    <View
      style={styles.root}
      accessible={!waking}
      accessibilityRole="progressbar"
      accessibilityLabel="Loading conversation"
      accessibilityLiveRegion="polite"
    >
      {waking ? (
        <>
          <TappableCat
            state="waking"
            size={CAT_SIZE}
            animate={waking.animate}
            stillPortrait={reduced}
            onPress={tap}
            accessibilityLabel="Brain: Waking up"
            accessibilityHint="Brain answers"
            style={styles.cat}
          />
          <Text style={[styles.line, { color: chrome.textMuted }]} accessibilityLiveRegion="polite">
            {said ?? "Waking up"}
          </Text>
        </>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  // Matches InterfaceTimelineEmptyState's stack, so the hand-off to "Ready
  // when you are" keeps the cat in place.
  root: {
    flexGrow: 1,
    minHeight: 240,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 36,
    paddingVertical: 24,
  },
  cat: {
    marginBottom: 22,
  },
  line: {
    ...TypeScale.label,
    textAlign: "center",
  },
});
