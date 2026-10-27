import React, { useMemo } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { Stack, useLocalSearchParams, useRouter } from "expo-router";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContentWidth, ContinuousCorners, Radii, useAppTheme } from "../../constants/tokens";
import { AppText } from "../../components/ui/AppText";
import { EmptyState } from "../../components/ui/EmptyState";
import { ListRow, ListSection } from "../../components/ui/ListSection";
import { openAboutPage } from "../../services/aboutPages";
import {
  findLicenseItem,
  licenseItemId,
  licenseSummary,
  openSourceLicenses,
} from "../../services/openSourceLicenses";

/** One open-source work: its license text, source, and what it includes. */
export default function OpenSourceLicenseScreen() {
  const { id } = useLocalSearchParams<{ id?: string }>();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { colors, theme } = useAppTheme();
  const data = useMemo(openSourceLicenses, []);
  const item = findLicenseItem(data, id);

  if (!item) {
    return (
      <View style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
        <EmptyState icon="document-text" title="License not found" />
      </View>
    );
  }
  const text = item.text ? data.texts[item.text] : null;
  const url = item.url;

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
    >
      <Stack.Screen options={{ title: item.name }} />
      <View style={styles.inner}>
        <AppText variant="title" accessibilityRole="header">{item.name}</AppText>
        <AppText tone="secondary" style={styles.summary}>{licenseSummary(item)}</AppText>
        {url ? (
          <ListSection>
            <ListRow
              title="Source"
              subtitle={url.replace(/^https?:\/\//, "")}
              icon="open-external"
              accessibilityRole="link"
              onPress={() => void openAboutPage(url)}
            />
          </ListSection>
        ) : null}
        {item.note ? <AppText tone="secondary" style={styles.note}>{item.note}</AppText> : null}
        {text ? (
          <View style={[styles.textBox, { backgroundColor: theme.materials.tint, borderColor: colors.border }]}>
            <AppText variant="mono" selectable style={styles.text}>{text}</AppText>
          </View>
        ) : null}
        {item.includes?.length ? (
          <ListSection title={`Includes ${item.includes.length} packages`} style={styles.includes}>
            {item.includes.map((included, includedIndex) => (
              <ListRow
                key={`${included.name}@${included.version ?? ""}`}
                title={included.name}
                subtitle={licenseSummary(included)}
                accessory="chevron"
                onPress={() =>
                  router.push({
                    pathname: "/settings/license",
                    params: { id: licenseItemId(...String(id).split("/").map(Number), includedIndex) },
                  })
                }
              />
            ))}
          </ListSection>
        ) : null}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 20 },
  inner: { width: "100%", maxWidth: ContentWidth.page, alignSelf: "center" },
  summary: { marginTop: 4, marginBottom: 20 },
  note: { paddingHorizontal: 4, marginBottom: 16 },
  textBox: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.card,
    ...ContinuousCorners,
    padding: 14,
  },
  text: { fontSize: 12, lineHeight: 17 },
  includes: { marginTop: 24 },
});
