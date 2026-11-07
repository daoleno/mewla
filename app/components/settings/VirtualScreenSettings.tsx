import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContentWidth, useAppTheme } from "../../constants/tokens";
import { ListRow, ListSection } from "../ui";
import { InkSwitch } from "../ui/InkSwitch";
import { useVirtualScreenLabs } from "../../store/virtualScreenLabs";

/** Settings → Labs → Virtual screen: the one switch. What it does is shown on Screen. */
export function VirtualScreenSettings() {
  const insets = useSafeAreaInsets();
  const { colors } = useAppTheme();
  const labs = useVirtualScreenLabs();
  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
    >
      <View style={styles.inner}>
        <ListSection>
          <ListRow
            title="Virtual screen"
            icon="desktop"
            accessibilityRole="switch"
            trailing={<InkSwitch value={labs.enabled} disabled={!labs.ready} accessibilityLabel="Virtual screen" onValueChange={(next) => void labs.set(next).catch(() => {})} />}
          />
        </ListSection>
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: { alignItems: "center", paddingTop: 12 },
  inner: { width: "100%", maxWidth: ContentWidth.page, gap: 12 },
});
