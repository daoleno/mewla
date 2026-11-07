import React, { useEffect } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useAppColors } from "../../constants/tokens";
import { pathMark, type TransportPathInfo } from "../../services/connectionPath";
import { ComputerRow } from "../navigation/PrimaryDrawerPanel";
import { ComputersSection } from "../settings/ComputersSection";
import { AppText } from "../ui/AppText";
import { useToast } from "../ui/Toast";

const NOOP = () => undefined;

const FIXTURES: Record<string, TransportPathInfo | null> = {
  lan: { path: "lan", host: "192.168.1.20", rttMs: 12 },
  tailscale: { path: "tailscale", host: "100.101.102.103", rttMs: 28 },
  relay: { path: "relay", host: null, rttMs: 140 },
  none: null,
};

/** The menu's computer row and Settings → Computers, on one connection path. */
export function ConnectionPathScreenshotDemo({ fixture, reveal, header }: { fixture?: string; reveal?: string; header: (title: string) => React.ReactNode }) {
  const colors = useAppColors();
  const toast = useToast();
  const mark = pathMark(FIXTURES[fixture ?? "lan"] ?? null, true);
  const show = () => mark && toast.show({ title: mark.detail, tone: "info" });
  // The long press itself is a device gesture; `reveal=1` shows what it shows.
  useEffect(() => {
    if (reveal === "1") show();
  }, []);
  return (
    <ScrollView style={{ flex: 1, backgroundColor: colors.bgPrimary }} contentContainerStyle={styles.content}>
      {header("Settings")}
      <View style={styles.inner}>
        <AppText variant="label" tone="tertiary" style={styles.header}>Menu</AppText>
        <View style={[styles.footer, { borderColor: colors.borderSubtle }]}>
          <ComputerRow
            drawerVisible
            name="manjaro"
            status=""
            tone="quiet"
            mark={mark}
            switchable
            expanded={false}
            onPress={NOOP}
            onShowPath={show}
          />
        </View>
        <AppText variant="label" tone="tertiary" style={styles.header}>Computers</AppText>
        <ComputersSection
          computers={[
            { id: "mac", name: "daoleno's MacBook Pro", status: "Online" },
            { id: "linux", name: "manjaro", status: "In use", mark },
          ]}
          onOpen={NOOP}
          onPair={NOOP}
          onShowPath={show}
        />
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 20 },
  inner: { width: "100%", maxWidth: 760, alignSelf: "center" },
  header: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 6 },
  footer: { borderTopWidth: StyleSheet.hairlineWidth, paddingTop: 6, marginBottom: 12 },
});
