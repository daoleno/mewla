import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Modal,
  Pressable,
  StyleSheet,
  View,
  type AlertButton,
  type AlertOptions,
} from "react-native";
import {
  ContinuousCorners,
  useAppColors,
  type AppColors,
} from "../../constants/tokens";
import { AppText } from "./AppText";
import { Button } from "./Button";

interface PendingAlert {
  id: number;
  title: string;
  message?: string;
  buttons: AlertButton[];
  options?: AlertOptions;
}

let nextAlertId = 0;
let pendingAlerts: PendingAlert[] = [];
const listeners = new Set<(alerts: PendingAlert[]) => void>();

function publish(next: PendingAlert[]) {
  pendingAlerts = next;
  for (const listener of listeners) listener(pendingAlerts);
}

// react-native-web ships Alert.alert as a no-op; every confirmation in the
// app goes through it, so web presents the same buttons in an in-app dialog.
Alert.alert = (title, message, buttons, options) => {
  publish([
    ...pendingAlerts,
    {
      id: ++nextAlertId,
      title,
      message,
      buttons: buttons?.length ? buttons : [{ text: "OK" }],
      options,
    },
  ]);
};

function settle(alert: PendingAlert, button: AlertButton | null) {
  publish(pendingAlerts.filter((pending) => pending.id !== alert.id));
  if (button) {
    button.onPress?.();
  } else {
    alert.options?.onDismiss?.();
  }
}

export function AlertHost() {
  const [alerts, setAlerts] = useState(pendingAlerts);
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);

  useEffect(() => {
    listeners.add(setAlerts);
    setAlerts(pendingAlerts);
    return () => {
      listeners.delete(setAlerts);
    };
  }, []);

  const alert = alerts[0];
  if (!alert) return null;

  const cancelButton = alert.buttons.find((button) => button.style === "cancel") ?? null;
  const dismiss = () => {
    if (cancelButton) {
      settle(alert, cancelButton);
    } else if (alert.options?.cancelable) {
      settle(alert, null);
    }
  };
  const ordered = cancelButton
    ? [cancelButton, ...alert.buttons.filter((button) => button !== cancelButton)]
    : alert.buttons;
  const stacked = ordered.length > 2;

  return (
    <Modal transparent visible animationType="fade" onRequestClose={dismiss}>
      <View style={styles.root}>
        <Pressable
          accessibilityLabel="Dismiss dialog"
          style={[StyleSheet.absoluteFill, { backgroundColor: colors.modalBackdrop }]}
          onPress={dismiss}
        />
        <View accessibilityRole="alert" style={styles.card}>
          <AppText variant="title">{alert.title}</AppText>
          {alert.message ? (
            <AppText variant="body" tone="secondary" style={styles.message}>
              {alert.message}
            </AppText>
          ) : null}
          <View style={[styles.actions, stacked && styles.actionsStacked]}>
            {ordered.map((button, index) => (
              <Button
                key={`${index}:${button.text ?? ""}`}
                label={button.text ?? "OK"}
                size="sm"
                block={stacked}
                haptic={false}
                variant={
                  button.style === "destructive"
                    ? "destructive"
                    : button.style === "cancel"
                      ? "outlined"
                      : "filled"
                }
                onPress={() => settle(alert, button)}
              />
            ))}
          </View>
        </View>
      </View>
    </Modal>
  );
}

function createStyles(colors: AppColors) {
  return StyleSheet.create({
    root: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      padding: 24,
    },
    card: {
      ...ContinuousCorners,
      width: "100%",
      maxWidth: 420,
      padding: 20,
      borderRadius: 18,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderStrong,
      backgroundColor: colors.modalSurface,
    },
    message: {
      marginTop: 8,
    },
    actions: {
      marginTop: 20,
      flexDirection: "row",
      justifyContent: "flex-end",
      gap: 10,
    },
    actionsStacked: {
      flexDirection: "column",
      alignItems: "stretch",
    },
  });
}
