import React from "react";
import { StyleSheet, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { Button } from "../ui/Button";
import { Icon, type IconName } from "../icons/Icon";

interface FriendsSheetProps {
  visible: boolean;
  title: string;
  /** What will happen, one line each. */
  points: readonly { icon: IconName; text: string }[];
  confirmLabel: string;
  cancelLabel: string;
  destructive?: boolean;
  busy?: boolean;
  onConfirm(): void;
  onClose(): void;
}

/** The confirm sheet for turning on auto-post and deleting the Friends account. */
export function FriendsSheet({ visible, title, points, confirmLabel, cancelLabel, destructive, busy, onConfirm, onClose }: FriendsSheetProps) {
  const { colors } = useAppTheme();
  return (
    <BottomSheetFrame visible={visible} onClose={onClose} contentStyle={styles.content}>
      <AppText variant="sheetTitle" accessibilityRole="header">
        {title}
      </AppText>
      <View style={styles.points}>
        {points.map((point) => (
          <View key={point.text} style={styles.point}>
            <Icon name={point.icon} color={colors.textSecondary} size={22} />
            <AppText variant="body" tone="secondary" style={styles.pointText}>
              {point.text}
            </AppText>
          </View>
        ))}
      </View>
      <Button label={confirmLabel} size="lg" block variant={destructive ? "seal" : "filled"} loading={busy} onPress={onConfirm} />
      <Button label={cancelLabel} size="lg" block variant="outlined" disabled={busy} onPress={onClose} style={styles.cancel} />
    </BottomSheetFrame>
  );
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: 20, paddingBottom: 20, gap: 8 },
  points: { gap: 18, paddingVertical: 16 },
  point: { flexDirection: "row", alignItems: "flex-start", gap: 14 },
  pointText: { flex: 1 },
  cancel: { marginTop: 4 },
});
