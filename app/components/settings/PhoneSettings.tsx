import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useRouter } from "expo-router";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContentWidth, useAppTheme } from "../../constants/tokens";
import { ListRow, ListSection } from "../ui";
import { InkSwitch } from "../plugins/PluginConnectionViews";
import { usePhoneUse } from "../../store/phoneUse";

/** Settings → Labs → Phone use: the switch, then where it stands. */
export function PhoneSettings() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { colors } = useAppTheme();
  const phone = usePhoneUse();

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
    >
      <View style={styles.inner}>
        <ListSection>
          <ListRow
            title="Phone use"
            icon="phone"
            accessibilityRole="switch"
            trailing={
              <InkSwitch
                value={phone.enabled}
                disabled={!phone.available}
                accessibilityLabel="Phone use"
                onValueChange={(next) => {
                  if (next) {
                    void phone.turnOn();
                    router.push("/phone");
                  } else void phone.turnOff();
                }}
              />
            }
          />
          {phone.enabled ? (
            <ListRow
              title="Open Phone use"
              subtitle={phoneStateWord(phone.ready, phone.status?.state)}
              icon="phone"
              accessory="chevron"
              onPress={() => router.push("/phone")}
            />
          ) : null}
        </ListSection>
      </View>
    </ScrollView>
  );
}

export function phoneStateWord(ready: boolean, state: string | undefined): string {
  if (!ready) return "Set up";
  if (state === "person_in_control") return "You are in control";
  return "Ready";
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 16 },
  inner: { width: "100%", maxWidth: ContentWidth.page, alignSelf: "center" },
});
