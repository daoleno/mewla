import React, { useEffect, useRef } from "react";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import { MewlaPhone, PhoneLiveView, type PhoneStatus } from "../../modules/mewla-phone/src";
import { usePhoneUse } from "../../store/phoneUse";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";
import { ListRow, ListSection } from "../ui";
import { Icon } from "../icons/Icon";
import { IconButton } from "../ui/IconButton";
import { phoneMode, phoneSetupPlan, phoneSteps, type PhoneStepId } from "./phoneSetup";

/** Phone use: the setup until it works, then the agent's own screen with Take over and Stop one tap away. */
export function PhoneScreen() {
  const { colors } = useAppTheme();
  const phone = usePhoneUse();
  const { watchSetup, status } = phone;
  useEffect(() => watchSetup(), [watchSetup]);

  // Once paired, the helper starts by itself: nothing for the person to press.
  const starting = useRef(false);
  useEffect(() => {
    if (!MewlaPhone || !status?.enabled || !status.paired || status.helper || starting.current) return;
    starting.current = true;
    void MewlaPhone.ensureHelper().finally(() => {
      starting.current = false;
    });
  }, [status?.enabled, status?.paired, status?.helper]);

  if (!phone.available || !status) {
    return <View style={[styles.fill, { backgroundColor: colors.bgPrimary }]} />;
  }
  if (!status.enabled) {
    return (
      <SafeAreaView style={[styles.fill, styles.center, { backgroundColor: colors.bgPrimary }]} edges={["bottom"]}>
        <Icon name="phone" color={colors.textTertiary} size={44} />
        <Button label="Turn on" variant="filled" size="lg" onPress={() => void phone.turnOn()} style={styles.cta} />
      </SafeAreaView>
    );
  }
  const mode = phoneMode(status);
  return <Body status={status} mode={mode} />;
}

function Body({ status, mode }: { status: PhoneStatus; mode: ReturnType<typeof phoneMode> }) {
  const { colors } = useAppTheme();
  return (
    <SafeAreaView style={[styles.fill, { backgroundColor: colors.bgPrimary }]} edges={["bottom"]}>
      {mode === "setup" ? <Setup status={status} /> : <Live status={status} mode={mode} />}
    </SafeAreaView>
  );
}

function Setup({ status }: { status: PhoneStatus }) {
  const { colors } = useAppTheme();
  const phone = usePhoneUse();
  const steps = phoneSteps(status);
  const [running, setRunning] = React.useState(false);
  const plan = phoneSetupPlan(status, running);

  // The guided setup runs in the background of this page: Mewla opens Settings, taps through it and
  // brings the person back here; this page only starts it and shows where it is.
  const setUp = async () => {
    if (!MewlaPhone) return;
    setRunning(true);
    try {
      await phone.pair();
    } finally {
      setRunning(false);
    }
  };

  const run = async (id: PhoneStepId) => {
    if (!MewlaPhone) return;
    if (id === "developer") MewlaPhone.openDeveloperSettings();
    else if (id === "wifi" || id === "wireless") MewlaPhone.openWirelessDebugging();
    else if (id === "pair") {
      setRunning(true);
      try {
        MewlaPhone.openWirelessDebugging();
        await phone.pair();
      } finally {
        setRunning(false);
      }
    } else if (id === "helper") await MewlaPhone.ensureHelper();
  };

  const primary = async () => {
    if (!MewlaPhone) return;
    if (plan.action === "allow") {
      // Silent when the secure-settings grant exists; otherwise the system's page for Mewla's service.
      if (!MewlaPhone.enableAccessibility()) MewlaPhone.openAccessibilitySettings();
    } else if (plan.action === "setup") await setUp();
    else if (plan.action === "wifi") MewlaPhone.openWirelessDebugging();
    else await MewlaPhone.ensureHelper();
  };

  return (
    <View style={styles.setup}>
      <View style={styles.hero}>
        <Icon name="phone" color={colors.textPrimary} size={40} />
      </View>
      <ListSection>
        {steps.map((step) => {
          const current = plan.current === step.id;
          const busy = current && !step.done && (running || status.guide !== null || step.id === "helper");
          return (
            <ListRow
              key={step.id}
              title={step.title}
              subtitle={current ? plan.note ?? step.hint : undefined}
              onPress={step.done ? undefined : () => void run(step.id)}
              trailing={
                step.done ? (
                  <Icon name="check-circle-fill" color={colors.success} size={22} />
                ) : busy ? (
                  <ActivityIndicator color={colors.textTertiary} />
                ) : (
                  <Icon name="circle" color={colors.textTertiary} size={22} />
                )
              }
            />
          );
        })}
      </ListSection>
      <View style={styles.footer}>
        <Button label={plan.label} variant="filled" size="lg" block loading={(running || status.guide !== null) && plan.action === "setup"} onPress={() => void primary()} />
        {plan.action === "allow" && status.restricted ? (
          <Button label="Open App info" variant="plain" size="md" onPress={() => MewlaPhone?.openAppSettings()} />
        ) : null}
      </View>
    </View>
  );
}

function Live({ status, mode }: { status: PhoneStatus; mode: Exclude<ReturnType<typeof phoneMode>, "setup"> }) {
  const { colors } = useAppTheme();
  const phone = usePhoneUse();
  const ratio = status.w > 0 && status.h > 0 ? status.w / status.h : 9 / 20;
  const driving = mode === "driving" || mode === "handoff";

  return (
    <View style={styles.fill}>
      <View style={styles.title}>
        <AppText variant="title">
          {mode === "handoff" ? status.handoff : mode === "stopped" ? "Stopped" : mode === "idle" ? "Ready" : status.agent ?? ""}
        </AppText>
        <View style={[styles.dot, { backgroundColor: mode === "idle" ? colors.success : driving ? colors.seal : colors.accent }]} />
        {mode === "watching" || mode === "driving" ? (
          <IconButton
            icon="expand"
            size={36}
            iconSize={18}
            color={colors.textSecondary}
            accessibilityLabel="Float"
            onPress={() => MewlaPhone?.enterPip?.(status.w, status.h)}
          />
        ) : null}
      </View>
      <View style={styles.stage}>
        {PhoneLiveView && status.w > 0 && mode !== "idle" ? (
          <View style={[styles.display, { aspectRatio: ratio, borderColor: colors.borderSubtle }]}>
            <PhoneLiveView style={styles.fill} interactive={driving} />
          </View>
        ) : (
          <Icon name="check-circle" color={colors.textTertiary} size={56} />
        )}
      </View>
      <View style={styles.bar}>
        {mode === "watching" ? (
          <>
            <Button label="Take over" icon="hand" variant="filled" size="lg" style={styles.grow} onPress={phone.takeOver} />
            <Button label="Stop" icon="stop" variant="destructive" size="lg" onPress={phone.stop} />
          </>
        ) : null}
        {mode === "driving" ? (
          <>
            <Button label="Give back" icon="return-forward" variant="filled" size="lg" style={styles.grow} onPress={phone.giveBack} />
            <Button label="Stop" icon="stop" variant="destructive" size="lg" onPress={phone.stop} />
          </>
        ) : null}
        {mode === "handoff" ? (
          <>
            <Button label="Done" icon="check" variant="filled" size="lg" style={styles.grow} onPress={phone.handoffDone} />
            <Button label="Stop" icon="stop" variant="destructive" size="lg" onPress={phone.stop} />
          </>
        ) : null}
        {mode === "stopped" ? (
          <Button label="Resume" icon="play" variant="filled" size="lg" style={styles.grow} onPress={phone.giveBack} />
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  center: { alignItems: "center", justifyContent: "center", gap: 24 },
  cta: { minWidth: 180 },
  setup: { flex: 1, paddingHorizontal: 16 },
  hero: { alignItems: "center", paddingVertical: 28 },
  footer: { marginTop: "auto", gap: 8, paddingBottom: 12, alignItems: "stretch" },
  title: { flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8, paddingVertical: 12 },
  dot: { width: 8, height: 8, borderRadius: 4 },
  stage: { flex: 1, alignItems: "center", justifyContent: "center", paddingHorizontal: 16 },
  display: { maxHeight: "100%", maxWidth: "100%", width: "100%", borderRadius: 20, overflow: "hidden", borderWidth: StyleSheet.hairlineWidth },
  bar: { flexDirection: "row", gap: 12, paddingHorizontal: 16, paddingVertical: 12 },
  grow: { flex: 1 },
});
