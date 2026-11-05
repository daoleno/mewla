import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContentWidth, useAppTheme } from "../../constants/tokens";
import { AppText, Button, ListRow, ListSection } from "../ui";
import { InkSwitch } from "../plugins/PluginConnectionViews";
import { useIPhoneUse } from "../../store/iphoneUse";

/**
 * Settings → Labs → iPhone use: the current Mac's switch, where it stands, and the agent using
 * the iPhone now with Done (for what it asked of the person) and Stop.
 */
export function IPhoneSettings() {
  const insets = useSafeAreaInsets();
  const { colors } = useAppTheme();
  const iphone = useIPhoneUse(3_000);
  const status = iphone.status;
  const unsupported = status?.state === "unsupported";
  const active = status?.active;

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
    >
      <View style={styles.inner}>
        <ListSection>
          <ListRow
            title="iPhone use"
            icon="phone"
            accessibilityRole="switch"
            trailing={
              <InkSwitch
                value={!!status?.enabled}
                disabled={!status || unsupported || iphone.busy}
                accessibilityLabel="iPhone use"
                onValueChange={(next) => void iphone.change({ enabled: next })}
              />
            }
          />
        </ListSection>
        <AppText variant="body" style={[styles.note, { color: colors.textSecondary }]}>
          {status
            ? status.summary
            : iphone.error
              ? `This computer doesn't offer iPhone use (${iphone.error}).`
              : "Reading this computer…"}
        </AppText>
        {active ? (
          <ListSection>
            <ListRow title={`${active.name || active.worker} is using your iPhone`} icon="phone" />
            <View style={styles.actions}>
              <Button label="Done" icon="check" size="sm" variant="filled" accessibilityLabel="Done: tell the agent you finished"
                onPress={() => void iphone.change({ handoff_done: true })} disabled={iphone.busy} />
              <Button label="Stop" size="sm" variant="outlined" accessibilityLabel={`Stop ${active.name || active.worker}`}
                onPress={() => void iphone.change({ stop_agent: true })} disabled={iphone.busy} />
            </View>
          </ListSection>
        ) : null}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 16 },
  inner: { width: "100%", maxWidth: ContentWidth.page, alignSelf: "center", gap: 12 },
  note: { paddingHorizontal: 4 },
  actions: { flexDirection: "row", gap: 8, paddingHorizontal: 16, paddingBottom: 12 },
});
