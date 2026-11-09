import React from "react";
import { StyleSheet, View } from "react-native";
import type { SwitchProps } from "react-native";
import { InkSwitch } from "./InkSwitch";
import { Spinner } from "./Spinner";

/** Roughly the footprint of a platform switch, so a row does not jump while it waits. */
const WIDTH = 52;
const HEIGHT = 32;

interface PendingSwitchProps extends SwitchProps {
  /** The change is on its way to the computer: the switch gives way to a spinner at once. */
  pending?: boolean;
}

/**
 * A switch whose change takes a round trip to the computer. The instant it is
 * tapped it becomes a spinner, and it returns showing what the computer
 * confirmed (or the old state, if the change failed).
 */
export function PendingSwitch({ pending = false, accessibilityLabel, ...props }: PendingSwitchProps) {
  if (pending) {
    return (
      <View
        style={styles.pending}
        accessible
        accessibilityRole="switch"
        accessibilityLabel={accessibilityLabel}
        accessibilityState={{ busy: true, disabled: true }}
      >
        <Spinner />
      </View>
    );
  }
  return <InkSwitch accessibilityLabel={accessibilityLabel} {...props} />;
}

const styles = StyleSheet.create({
  pending: { width: WIDTH, height: HEIGHT, alignItems: "center", justifyContent: "center" },
});
