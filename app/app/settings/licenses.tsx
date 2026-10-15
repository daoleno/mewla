import React, { useMemo } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useRouter } from "expo-router";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "../../components/ui/AppText";
import { ListRow, ListSection } from "../../components/ui/ListSection";
import { licenseItemId, licenseSummary, openSourceLicenses } from "../../services/openSourceLicenses";

/** Settings → Open-source licenses: every third-party work in the app. */
export default function OpenSourceLicensesScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { colors } = useAppTheme();
  const data = useMemo(openSourceLicenses, []);

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
    >
      <View style={styles.inner}>
        <AppText tone="secondary" style={styles.intro}>
          Mewla is built on these open-source projects. Tap one to read its license.
        </AppText>
        {data.sections.map((section, sectionIndex) => (
          <ListSection key={section.title} title={section.title}>
            {section.items.map((item, itemIndex) => (
              <ListRow
                key={`${item.name}@${item.version ?? ""}`}
                title={item.name}
                subtitle={licenseSummary(item)}
                accessory="chevron"
                onPress={() =>
                  router.push({
                    pathname: "/settings/license",
                    params: { id: licenseItemId(sectionIndex, itemIndex) },
                  })
                }
              />
            ))}
          </ListSection>
        ))}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 16 },
  inner: { width: "100%", maxWidth: 760, alignSelf: "center" },
  intro: { paddingHorizontal: 4, marginBottom: 20 },
});
