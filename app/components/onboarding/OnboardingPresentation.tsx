import React, { useState } from "react";
import { Linking, ScrollView, StyleSheet, Text, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { TypeScale, Typography, useAppColors } from "../../constants/tokens";
import { MewlaMark } from "../mewla/MewlaMark";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { PetSprite } from "../pets/PetSprite";
import { Button } from "../ui/Button";
import { EmptyState } from "../ui/EmptyState";
import { Icon } from "../icons/Icon";

const GUIDE = "https://github.com/daoleno/mewla/blob/main/docs/";
export const COMPUTER_SETUP_STEPS = [
  { title: "Check your computer", command: "mewla doctor" },
  { title: "Start Mewla on trusted Wi-Fi", command: "mewla --lan" },
  { title: "Run mewla pair, then scan its code" },
] as const;

export function OnboardingPresentation({ serverName, connection, error, onPair, onRetry, onSettings, onContinue }: {
  serverName?: string;
  connection?: string;
  error?: string;
  onPair(mode: "scanner" | "editor"): void;
  onRetry(): void;
  onSettings(): void;
  onContinue(): void;
}) {
  const colors = useAppColors();
  const [setup, setSetup] = useState(false);
  const paired = connection !== undefined;
  const connected = connection === "connected";
  const connecting = connection === "connecting";
  return (
    <SafeAreaView style={[styles.screen, { backgroundColor: colors.bgPrimary }]} edges={["top", "bottom"]}>
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.brand}>
          <MewlaMark size={44} />
          <Text style={[styles.brandName, { color: colors.textPrimary }]}>Mewla</Text>
        </View>
        {paired ? (
          <EmptyState
            title={connected ? "Your computer is connected" : connecting ? "Connecting to your computer" : "Your server is offline"}
            detail={error || serverName}
            icon={connected ? "check-circle" : "server"}
            busy={connecting}
            // The cat's first moment: it moves into the seal once paired.
            art={<PetSprite state={connected ? "idle" : connecting ? "waking" : "offline"} size={120} />}
            action={connected ? { label: "Open Brain", icon: "arrow-forward", onPress: onContinue } : connecting ? undefined : { label: "Retry connection", icon: "refresh", onPress: onRetry }}
            secondary={!connected ? { label: "Server settings", icon: "settings", onPress: onSettings } : undefined}
          />
        ) : (
          <>
            <View style={styles.heading}>
              {/* An empty seal until a computer is paired: Brain has no home yet. */}
              <PetSprite state="homeless" size={120} />
              <Text accessibilityRole="header" style={[styles.title, { color: colors.textPrimary }]}>Give Brain a home</Text>
              <Text style={[styles.lede, { color: colors.textSecondary }]}>Brain lives on your computer. Pair this phone with it once.</Text>
            </View>
            <View style={styles.actions}>
              <Button variant="filled" size="lg" block icon="qr-code" label="Scan pairing code"
                onPress={() => onPair("scanner")} />
              <Button variant="outlined" size="lg" block icon="link" label="Import pairing link"
                onPress={() => onPair("editor")} />
            </View>
            <View style={[styles.setup, { borderColor: colors.borderSubtle }]}>
              <AnimatedPressable accessibilityRole="button" accessibilityLabel="Computer setup"
                accessibilityState={{ expanded: setup }} onPress={() => setSetup(!setup)} style={styles.setupHeader}>
                <Icon name="desktop" size={20} color={colors.textSecondary} />
                <Text style={[styles.setupTitle, { color: colors.textPrimary }]}>Computer setup</Text>
                <Icon name={setup ? "chevron-up" : "chevron-down"} size={18} color={colors.textSecondary} />
              </AnimatedPressable>
              {setup ? <View style={styles.steps}>
                <AnimatedPressable accessibilityRole="link" accessibilityLabel="Install Mewla on your computer"
                  onPress={() => void Linking.openURL(GUIDE + "install-daemon.md")} style={styles.link}>
                  <Icon name="download" size={18} color={colors.accent} />
                  <Text style={[styles.linkText, { color: colors.accent }]}>Install Mewla</Text>
                </AnimatedPressable>
                {COMPUTER_SETUP_STEPS.map((step, index) => (
                  <View key={step.title} style={styles.step}>
                    <Text style={[styles.number, { color: colors.textTertiary }]}>{index + 1}</Text>
                    <View style={styles.stepContent}>
                      <Text style={[styles.stepTitle, { color: colors.textPrimary }]}>{step.title}</Text>
                      {"command" in step ? <Text selectable style={[styles.command, { color: colors.textSecondary }]}>{step.command}</Text> : null}
                    </View>
                  </View>
                ))}
                <AnimatedPressable accessibilityRole="link" accessibilityLabel="Remote HTTPS connection guide"
                  onPress={() => void Linking.openURL(GUIDE + "connect-and-pair.md")} style={styles.link}>
                  <Icon name="open-external" size={18} color={colors.accent} />
                  <Text style={[styles.linkText, { color: colors.accent }]}>Reach your computer from anywhere</Text>
                </AnimatedPressable>
              </View> : null}
            </View>
          </>
        )}
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  content: { flexGrow: 1, width: "100%", maxWidth: 520, alignSelf: "center", padding: 24, paddingBottom: 32 },
  brand: { flexDirection: "row", alignItems: "center", gap: 12, paddingVertical: 18 },
  brandName: { ...TypeScale.heading, fontSize: 27, letterSpacing: 0 },
  heading: { gap: 10, paddingTop: 32, paddingBottom: 32, alignItems: "center" },
  title: { ...TypeScale.largeTitle, marginTop: 14, textAlign: "center" },
  lede: { ...TypeScale.body, textAlign: "center", maxWidth: 300 },
  actions: { gap: 12 },
  setup: { borderTopWidth: StyleSheet.hairlineWidth, marginTop: 32 },
  setupHeader: { minHeight: 60, paddingVertical: 14, gap: 10, flexDirection: "row", alignItems: "center" },
  setupTitle: { ...TypeScale.label, flex: 1 },
  steps: { gap: 20, paddingBottom: 16 },
  step: { flexDirection: "row", gap: 14 },
  number: { ...TypeScale.label, width: 18 },
  stepContent: { flex: 1, gap: 6 },
  stepTitle: { ...TypeScale.body },
  command: { fontFamily: Typography.terminalFont, fontSize: 14, lineHeight: 22 },
  link: { flexDirection: "row", alignItems: "center", gap: 10, minHeight: 44 },
  linkText: { ...TypeScale.label, flexShrink: 1 },
});
