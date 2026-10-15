import React, { useEffect, useState } from "react";
import { ActivityIndicator, BackHandler, Linking, Platform, ScrollView, Share, StyleSheet, Text, View } from "react-native";
import * as Clipboard from "expo-clipboard";
import Animated, { FadeIn, FadeOut } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Radii, TypeScale, Typography, useAppColors } from "../../constants/tokens";
import type { BrainCatState } from "../mewla/brainCatState";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";
import { Icon } from "../icons/Icon";
import { OnboardingCat } from "./OnboardingCat";
import { OnboardingScanner } from "./OnboardingScanner";
import {
  INSTALL_COMMAND,
  INSTALL_NOTE,
  OTHER_WAYS_URL,
  START_COMMAND,
  START_NOTE,
  WELCOME_LEDE,
  WELCOME_TITLE,
  catBoxForStep,
  cornerCatBox,
  heroCatBox,
  previousOnboardingStep,
  type OnboardingStep,
} from "./onboardingModel";

export function OnboardingPresentation({ serverName, connection, error, pairing, initialStep = "welcome", onCode, onPaste, onRetry, onSettings, onContinue }: {
  serverName?: string;
  /** Set once a computer is paired: its connection state. */
  connection?: string;
  error?: string;
  /** A scanned or pasted code is pairing. */
  pairing: boolean;
  initialStep?: OnboardingStep;
  onCode(data: string): void;
  onPaste(): void;
  onRetry(): void;
  onSettings(): void;
  onContinue(): void;
}) {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const paired = connection !== undefined;
  const [step, setStep] = useState<OnboardingStep>(paired ? "connected" : initialStep);
  const [cameFrom, setCameFrom] = useState<OnboardingStep | null>(null);
  const [size, setSize] = useState<{ width: number; height: number } | null>(null);
  const go = (next: OnboardingStep) => {
    setCameFrom(step);
    setStep(next);
  };

  useEffect(() => {
    if (paired) setStep("connected");
  }, [paired]);

  useEffect(() => {
    if (Platform.OS !== "android") return;
    const subscription = BackHandler.addEventListener("hardwareBackPress", () => {
      const previous = previousOnboardingStep(step, cameFrom);
      if (!previous) return false;
      setCameFrom(null);
      setStep(previous);
      return true;
    });
    return () => subscription.remove();
  }, [cameFrom, step]);

  const frame = size ? { ...size, top: insets.top, bottom: insets.bottom } : null;
  const hero = frame ? heroCatBox(frame) : null;
  const connected = connection === "connected";
  const connecting = connection === "connecting";
  const catState: BrainCatState = step === "welcome" ? "attention"
    : step === "install" ? "working"
    : step === "scan" ? (pairing ? "attention" : "delegating")
    : connected ? "attention" : connecting ? "delegating" : "offline";
  const header = (number: number, place: string, title: string) => (
    <View style={[styles.header, frame ? { minHeight: cornerCatBox(frame).cy + cornerCatBox(frame).size / 2 - insets.top } : null]}>
      <View style={styles.eyebrow}>
        <View style={[styles.number, { borderColor: colors.borderStrong }]}>
          <AppText variant="micro" tone="secondary">{number}</AppText>
        </View>
        <AppText variant="micro" tone="secondary">{place}</AppText>
      </View>
      <AppText variant="display" accessibilityRole="header" style={styles.stepTitle}>{title}</AppText>
    </View>
  );
  const belowHero = hero ? hero.cy + hero.size / 2 + 28 - insets.top : 260;

  return (
    <View
      style={[styles.screen, { backgroundColor: colors.bgPrimary, paddingTop: insets.top, paddingBottom: insets.bottom }]}
      onLayout={(event) => {
        const { width, height } = event.nativeEvent.layout;
        setSize((current) => (current?.width === width && current.height === height ? current : { width, height }));
      }}
    >
      <Animated.View key={step} entering={FadeIn.duration(240).delay(140)} exiting={FadeOut.duration(120)} style={styles.step}>
        {step === "welcome" ? (
          <View style={[styles.column, { paddingTop: belowHero }]}>
            <View style={styles.center}>
              <Text accessibilityRole="header" style={[styles.welcomeTitle, { color: colors.textPrimary }]}>{WELCOME_TITLE.replace(". ", ".\n")}</Text>
              <AppText tone="secondary" style={styles.lede}>{WELCOME_LEDE}</AppText>
            </View>
            <View style={styles.actions}>
              <Button variant="filled" size="lg" block label="Set up my computer" onPress={() => go("install")} />
              <Button variant="plain" label="Mewla already running? Scan its code" onPress={() => go("scan")} />
            </View>
          </View>
        ) : step === "install" ? (
          <View style={styles.column}>
            <ScrollView contentContainerStyle={styles.scroll}>
              {header(1, "ON YOUR COMPUTER", "Install Mewla")}
              <CommandCard />
              <AppText variant="caption" tone="tertiary">{INSTALL_NOTE}</AppText>
              <AppText tone="secondary" style={styles.then}>
                Then run <Text style={[styles.inlineCode, { color: colors.textPrimary, backgroundColor: colors.bgElevated }]}>{START_COMMAND}</Text>. {START_NOTE}
              </AppText>
              <AppText variant="compact" tone="tertiary">It prints a QR code. Keep it on screen for the next step.</AppText>
            </ScrollView>
            <View style={styles.actions}>
              <Button variant="filled" size="lg" block label="Next: scan the code" onPress={() => go("scan")} />
            </View>
          </View>
        ) : step === "scan" ? (
          <View style={styles.column}>
            <ScrollView contentContainerStyle={styles.scroll}>
              {header(2, "ON THIS PHONE", "Scan the code in your terminal")}
              <OnboardingScanner paused={pairing} onCode={onCode} />
              <View style={styles.status} accessibilityLiveRegion="polite">
                {pairing ? <ActivityIndicator size="small" color={colors.textSecondary} /> : null}
                <AppText variant="compact" tone="secondary">{pairing ? "Pairing…" : "Looking for a Mewla code…"}</AppText>
              </View>
            </ScrollView>
            <View style={styles.actions}>
              <Button variant="outlined" size="lg" block icon="clipboard" label="Paste a pairing link" disabled={pairing} onPress={onPaste} />
              <AnimatedPressable accessibilityRole="link" accessibilityLabel="Other ways to connect"
                onPress={() => void Linking.openURL(OTHER_WAYS_URL)} style={styles.link}>
                <AppText variant="label" tone="secondary">Other ways to connect</AppText>
                <Icon name="open-external" size={14} color={colors.textSecondary} />
              </AnimatedPressable>
            </View>
          </View>
        ) : (
          <View style={[styles.column, { paddingTop: belowHero }]}>
            <View style={styles.center} accessibilityLiveRegion="polite">
              <Text accessibilityRole="header" style={[styles.welcomeTitle, { color: colors.textPrimary }]}>
                {connected ? "Brain is home." : connecting ? "Connecting…" : "Can't reach your computer"}
              </Text>
              <AppText tone="secondary" style={styles.lede}>
                {connected ? `Connected to ${serverName || "your computer"}.` : connecting ? serverName : error || `Check that mewla is running on ${serverName || "your computer"}.`}
              </AppText>
              {connecting ? <ActivityIndicator color={colors.textSecondary} /> : null}
            </View>
            <View style={styles.actions}>
              {connected ? <Button variant="filled" size="lg" block icon="arrow-forward" label="Open Brain" onPress={onContinue} /> : null}
              {!connected && !connecting ? <Button variant="filled" size="lg" block icon="refresh" label="Retry connection" onPress={onRetry} /> : null}
              {!connected ? <Button variant="plain" icon="settings" label="Server settings" onPress={onSettings} /> : null}
            </View>
          </View>
        )}
      </Animated.View>
      {frame && hero ? (
        <OnboardingCat
          box={catBoxForStep(step, frame)}
          state={catState}
          seal={step === "welcome" || (step === "connected" && connected) ? hero : null}
        />
      ) : null}
    </View>
  );
}

/** The one command, with Copy and Send to computer (the system share sheet). */
function CommandCard() {
  const colors = useAppColors();
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1600);
    return () => clearTimeout(timer);
  }, [copied]);
  return (
    <View style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle }]}>
      <Text selectable style={[styles.command, { color: colors.textPrimary }]}>{INSTALL_COMMAND}</Text>
      <View style={styles.cardActions}>
        <Button size="sm" variant="tinted" icon={copied ? "check" : "copy"} label={copied ? "Copied" : "Copy"}
          onPress={() => void Clipboard.setStringAsync(INSTALL_COMMAND).then(() => setCopied(true))} />
        <Button size="sm" variant="tinted" icon="share" label="Send to computer"
          onPress={() => void Share.share({ message: INSTALL_COMMAND }).catch(() => undefined)} />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  step: { flex: 1 },
  column: { flex: 1, width: "100%", maxWidth: 520, alignSelf: "center", paddingHorizontal: 24, paddingBottom: 16 },
  scroll: { gap: 16, paddingBottom: 16 },
  center: { alignItems: "center", gap: 12, flex: 1 },
  welcomeTitle: { ...TypeScale.largeTitle, textAlign: "center" },
  lede: { textAlign: "center", maxWidth: 320 },
  actions: { gap: 8, alignItems: "stretch" },
  header: { gap: 10, paddingTop: 22, paddingRight: 100, justifyContent: "flex-end", paddingBottom: 6 },
  eyebrow: { flexDirection: "row", alignItems: "center", gap: 8 },
  number: { width: 20, height: 20, borderRadius: 10, borderWidth: StyleSheet.hairlineWidth, alignItems: "center", justifyContent: "center" },
  stepTitle: { fontSize: 30, lineHeight: 36 },
  card: { borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.lg, padding: 16, gap: 14 },
  command: { fontFamily: Typography.terminalFont, fontSize: 14, lineHeight: 21 },
  cardActions: { flexDirection: "row", gap: 8, flexWrap: "wrap" },
  then: { marginTop: 6 },
  inlineCode: { fontFamily: Typography.terminalFont, fontSize: 14 },
  status: { flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8, minHeight: 24 },
  link: { flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 6, minHeight: 44 },
});
