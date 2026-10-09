import React from "react";
import { StyleSheet, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import { PetSprite } from "../pets/PetSprite";

/**
 * App start, before the first screen can draw: the cat in its seal, one eye
 * open and an ear flicking, in place of a blank page or a spinner.
 */
export function CatSplash() {
  const { colors } = useAppTheme();
  return (
    <View
      style={[styles.fill, { backgroundColor: colors.bgPrimary }]}
      accessible
      accessibilityRole="progressbar"
      accessibilityLabel="Starting Mewla"
    >
      <PetSprite state="waking" size={96} />
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
