import { Alert } from "react-native";
import * as Haptics from "expo-haptics";

interface ConfirmDestructiveOptions {
  title: string;
  message?: string;
  /** Destructive button label, e.g. "Remove" or "Terminate". */
  confirmLabel: string;
  /** Dismiss label; "Cancel" unless the cancel word would be ambiguous ("Keep"). */
  cancelLabel?: string;
  onConfirm(): void | Promise<void>;
}

/**
 * The one confirmation for irreversible actions: a native alert whose
 * destructive button is never the default, preceded by a warning haptic.
 */
export function confirmDestructive({
  title,
  message,
  confirmLabel,
  cancelLabel = "Cancel",
  onConfirm,
}: ConfirmDestructiveOptions): void {
  void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning);
  Alert.alert(title, message, [
    { text: cancelLabel, style: "cancel" },
    {
      text: confirmLabel,
      style: "destructive",
      onPress: () => {
        void onConfirm();
      },
    },
  ]);
}
