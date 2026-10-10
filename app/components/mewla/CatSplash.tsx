import React from "react";
import { StyleSheet, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import { PetSprite } from "../pets/PetSprite";
import { useLoadingVeil } from "./useLoadingVeil";

/**
 * App start, before the first screen can draw: the cat hops out of its seal
 * in place of a blank page or a spinner. A start faster than the loading
 * delay shows only the background.
 */
export function CatSplash() {
  const { colors } = useAppTheme();
  const shown = useLoadingVeil(true) !== "waiting";
  return (
    <View
      style={[styles.fill, { backgroundColor: colors.bgPrimary }]}
      accessible
      accessibilityRole="progressbar"
      accessibilityLabel="Starting Mewla"
    >
      {shown ? <PetSprite state="waking" size={96} stillPortrait /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  fill: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
  },
});
