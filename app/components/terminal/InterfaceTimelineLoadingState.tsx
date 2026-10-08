import React, { useMemo } from "react";
import { StyleSheet, View, type DimensionValue } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { chromeTint } from "./composerMaterial";
import { BrainCatRow, useBrainCompanion, type BrainCompanion } from "../mewla/BrainCompanion";

/** A short exchange: your bubble on the right, Brain's lines on the left. */
const SKELETON: readonly { side: "you" | "brain"; widths: readonly DimensionValue[] }[] = [
  { side: "you", widths: ["46%"] },
  { side: "brain", widths: ["94%", "86%", "62%"] },
  { side: "you", widths: ["34%"] },
  { side: "brain", widths: ["90%", "48%"] },
];

/**
 * Brain's conversation while its history loads: a still outline of the
 * exchange where the messages will land, and the cat waking in the tail row
 * where it will sit once they do. Never the empty state's words, because
 * history may be on its way.
 */
export function InterfaceTimelineLoadingState({
  chrome,
}: {
  chrome: TerminalThemeChrome;
}) {
  const companion = useBrainCompanion();
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  // The cat waits for the history; a tap says so. When the Work column holds
  // the cat, the conversation shows none, so there is one cat on screen.
  const waking = useMemo<BrainCompanion | null>(
    () =>
      companion && !companion.presence.away
        ? {
            presence: { state: "waking" },
            animate: companion.animate,
            onCatTap: () => "Still waking up…",
          }
        : null,
    [companion],
  );
  return (
    <View style={styles.root}>
      {/* One element for the outline; the cat stays its own, so a screen
          reader can still tap it. */}
      <View
        style={styles.outline}
        accessible
        accessibilityRole="progressbar"
        accessibilityLabel="Loading conversation"
        accessibilityLiveRegion="polite"
      >
        {SKELETON.map((group, index) => (
          <View key={index} style={group.side === "you" ? styles.you : styles.brain}>
            {group.widths.map((width, line) => (
              <View
                key={line}
                style={[group.side === "you" ? styles.bubble : styles.line, { width }]}
              />
            ))}
          </View>
        ))}
      </View>
      {waking ? <BrainCatRow companion={waking} label="Waking up" chrome={chrome} /> : null}
    </View>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  const fill = chromeTint(chrome.text, 0.07, chrome.surfaceMuted);
  return StyleSheet.create({
    root: {
      flexGrow: 1,
      justifyContent: "center",
      gap: 22,
      paddingVertical: 24,
    },
    outline: {
      gap: 22,
    },
    you: {
      alignItems: "flex-end",
    },
    brain: {
      gap: 10,
    },
    bubble: {
      height: 40,
      borderRadius: 20,
      backgroundColor: fill,
    },
    line: {
      height: 10,
      borderRadius: 5,
      backgroundColor: fill,
    },
  });
}
