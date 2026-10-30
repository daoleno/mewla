import React from "react";
import { StyleSheet, View } from "react-native";
import { useRouter } from "expo-router";
import { useAppTheme } from "../../constants/tokens";
import { appVersion } from "../../constants/appVersion";
import { appUpdateRow, type AppUpdateState } from "../../services/appUpdate";
import { PRIVACY_POLICY_URL, SUPPORT_URL, openAboutPage } from "../../services/aboutPages";
import { useAppUpdate } from "../../services/useAppUpdate";
import { Button } from "../ui/Button";
import { ListRow, ListSection } from "../ui";
import { MewlaMark } from "../mewla/MewlaMark";

/** Settings → About: the version, its update when this build can take one, and the legal pages. */
export function AboutSection() {
  const { state, press } = useAppUpdate();
  return <AboutRows state={state} press={press} />;
}

/** The section for a given update state; Settings reads it from the app, previews and tests pass one in. */
export function AboutRows({ state, press }: { state: AppUpdateState; press: () => void }) {
  const router = useRouter();
  const { colors } = useAppTheme();
  const update = appUpdateRow(state);
  return (
    <ListSection>
      <ListRow
        title="Mewla"
        value={`Version ${appVersion}`}
        leading={<MewlaMark size={30} />}
        accessibilityLabel={`Mewla, Version ${appVersion}`}
      />
      {update ? (
        <ListRow
          title={update.title}
          subtitle={update.subtitle}
          value={update.value}
          icon={update.progress !== undefined ? "download" : "refresh"}
          loading={update.busy}
          onPress={update.actionable ? press : undefined}
          accessibilityLabel={[update.title, update.subtitle, update.value, update.action].filter(Boolean).join(", ")}
          trailing={
            update.action ? (
              <Button label={update.action} variant="filled" size="sm" onPress={press} />
            ) : update.progress !== undefined ? (
              <View style={[styles.track, { backgroundColor: colors.surfaceSubtle }]}>
                <View style={[styles.fill, { backgroundColor: colors.accent, width: `${Math.round(update.progress * 100)}%` }]} />
              </View>
            ) : undefined
          }
        />
      ) : null}
      <ListRow
        title="Privacy Policy"
        icon="shield-check"
        accessory="chevron"
        accessibilityRole="link"
        onPress={() => void openAboutPage(PRIVACY_POLICY_URL)}
      />
      <ListRow
        title="Support"
        icon="help"
        accessory="chevron"
        accessibilityRole="link"
        onPress={() => void openAboutPage(SUPPORT_URL)}
      />
      <ListRow
        title="Open-source Licenses"
        icon="document-text"
        accessory="chevron"
        onPress={() => router.push("/settings/licenses")}
      />
    </ListSection>
  );
}

const styles = StyleSheet.create({
  track: { width: 64, height: 4, borderRadius: 2, overflow: "hidden" },
  fill: { height: 4, borderRadius: 2 },
});
