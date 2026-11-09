import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContentWidth, useAppTheme } from "../../constants/tokens";
import { InlineNotice, ListRow, ListSection } from "../ui";
import { PendingSwitch } from "../ui/PendingSwitch";
import { useVirtualScreenLabs } from "../../store/virtualScreenLabs";

/**
 * The Virtual screen switch for the current computer, with what went wrong
 * under it. Settings → Labs and Screen on a Linux computer show this same row.
 */
export function VirtualScreenSwitch() {
  const labs = useVirtualScreenLabs();
  // Off until the computer answers; once it has failed to, a tap still tries.
  const waiting = !labs.ready && !labs.error;
  return (
    <View style={styles.switch}>
      <ListSection>
        <ListRow
          title="Virtual screen"
          subtitle={labs.busy ? (labs.target ? "Turning on…" : "Turning off…") : undefined}
          icon="desktop"
          accessibilityRole="switch"
          trailing={
            <PendingSwitch
              value={labs.enabled}
              pending={labs.busy}
              disabled={waiting}
              accessibilityLabel="Virtual screen"
              onValueChange={(next) => void labs.set(next)}
            />
          }
        />
      </ListSection>
      {labs.error ? (
        <InlineNotice title={labs.error} tone="danger" action={{ label: "Retry", onPress: labs.refresh }} />
      ) : null}
    </View>
  );
}

/** Settings → Labs → Virtual screen: the one switch. What it does is shown on Screen. */
export function VirtualScreenSettings() {
  const insets = useSafeAreaInsets();
  const { colors } = useAppTheme();
  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
    >
      <View style={styles.inner}>
        <VirtualScreenSwitch />
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 16 },
  inner: { width: "100%", maxWidth: ContentWidth.page, alignSelf: "center" },
  switch: { width: "100%", gap: 12 },
});
