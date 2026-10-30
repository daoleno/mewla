import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useIsFocused } from "expo-router";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContentWidth, useAppTheme } from "../../constants/tokens";
import { ListSection } from "../ui";
import { PetPicker } from "./PetPicker";

/** Settings → Pet: the preview and every pet to pick from. */
export function PetSettings() {
  const insets = useSafeAreaInsets();
  const { colors } = useAppTheme();
  const focused = useIsFocused();

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
    >
      <View style={styles.inner}>
        <ListSection>
          <PetPicker animate={focused} />
        </ListSection>
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 16 },
  inner: { width: "100%", maxWidth: ContentWidth.page, alignSelf: "center" },
});
