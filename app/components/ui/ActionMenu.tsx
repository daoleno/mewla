import React, { useEffect, useRef, useState } from "react";
import { Modal, Platform, Pressable, StyleSheet, View, useWindowDimensions } from "react-native";
import * as Haptics from "expo-haptics";
import { ContinuousCorners, Radii, TouchTarget, shadow, useAppTheme } from "../../constants/tokens";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import { MENU_POPOVER_WIDTH, menuPopoverPosition, recentTriggerRect, type AnchorRect } from "./menuAnchor";
import { AppText } from "./AppText";
import { BottomSheetFrame } from "./BottomSheetFrame";
import { Icon, type IconName } from "../icons/Icon";

export interface ActionMenuItem {
  key: string;
  label: string;
  icon: IconName;
  detail?: string;
  accessibilityLabel?: string;
  /** Small trailing adornment such as provider marks or a value. */
  trailing?: React.ReactNode;
  destructive?: boolean;
  disabled?: boolean;
  onPress(): void;
}

interface ActionMenuProps {
  visible: boolean;
  title?: string;
  items: readonly ActionMenuItem[];
  onClose(): void;
}

// iOS drops an Alert or modal presented while the sheet's own Modal is still
// being dismissed, so the chosen action runs only after the sheet is gone.
const IOS_DISMISS_SETTLE_MS = 80;

/**
 * Contextual action list. A bottom sheet on phones; on desktop web a popover
 * under the control that opened it, as menus are there. Closes before acting.
 */
export function ActionMenu({ visible, title, items, onClose }: ActionMenuProps) {
  const { colors, theme } = useAppTheme();
  const desktopWeb = useDesktopWeb();
  const pendingActionRef = useRef<(() => void) | null>(null);
  const [anchor, setAnchor] = useState<AnchorRect | null>(null);
  useEffect(() => {
    if (visible && desktopWeb) setAnchor(recentTriggerRect());
    if (!visible) setAnchor(null);
  }, [desktopWeb, visible]);
  useEffect(() => {
    if (visible || !pendingActionRef.current) return;
    const action = pendingActionRef.current;
    pendingActionRef.current = null;
    const timer = setTimeout(action, Platform.OS === "ios" ? IOS_DISMISS_SETTLE_MS : 0);
    return () => clearTimeout(timer);
  }, [visible]);
  if (desktopWeb && visible && anchor) {
    return (
      <MenuPopover
        anchor={anchor}
        items={items}
        onClose={onClose}
        onChoose={(item) => {
          pendingActionRef.current = item.onPress;
          onClose();
        }}
      />
    );
  }
  return (
    <BottomSheetFrame visible={visible} onClose={onClose} maxHeight="70%" desktopWidth={420}>
      {title ? (
        <AppText variant="label" tone="tertiary" accessibilityRole="header" style={styles.title}>
          {title}
        </AppText>
      ) : null}
      <View style={[styles.group, { backgroundColor: theme.materials.thin }]}>
        {items.map((item, index) => {
          const ink = item.disabled
            ? colors.disabledText
            : item.destructive
              ? colors.dangerText
              : colors.textPrimary;
          return (
            <Pressable
              key={item.key}
              disabled={item.disabled}
              accessibilityRole="button"
              accessibilityLabel={item.accessibilityLabel ?? item.label}
              accessibilityHint={item.detail}
              accessibilityState={{ disabled: Boolean(item.disabled) }}
              android_ripple={{ color: colors.surfacePressed }}
              onPress={() => {
                void Haptics.selectionAsync();
                pendingActionRef.current = item.onPress;
                onClose();
              }}
              style={({ pressed }) => [
                styles.item,
                index > 0 && {
                  borderTopWidth: StyleSheet.hairlineWidth,
                  borderTopColor: theme.materials.separator,
                },
                pressed && { backgroundColor: colors.surfacePressed },
              ]}
            >
              <Icon
                name={item.icon}
                size={20}
                color={item.destructive && !item.disabled ? colors.dangerText : item.disabled ? colors.disabledText : colors.textSecondary}
              />
              <View style={styles.copy}>
                <AppText variant="body" numberOfLines={1} style={{ color: ink }}>
                  {item.label}
                </AppText>
                {item.detail ? (
                  <AppText variant="caption" tone="tertiary" numberOfLines={2}>
                    {item.detail}
                  </AppText>
                ) : null}
              </View>
              {item.trailing ? <View style={styles.trailing}>{item.trailing}</View> : null}
            </Pressable>
          );
        })}
      </View>
    </BottomSheetFrame>
  );
}

function MenuPopover({
  anchor,
  items,
  onClose,
  onChoose,
}: {
  anchor: AnchorRect;
  items: readonly ActionMenuItem[];
  onClose(): void;
  onChoose(item: ActionMenuItem): void;
}) {
  const { colors } = useAppTheme();
  const viewport = useWindowDimensions();
  const [height, setHeight] = useState<number | null>(null);
  const position = menuPopoverPosition(anchor, height ?? 0, viewport);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <Modal visible transparent animationType="none" onRequestClose={onClose}>
      <Pressable accessibilityLabel="Close menu" style={StyleSheet.absoluteFill} onPress={onClose} />
      <View
        accessibilityRole="menu"
        onLayout={(event) => setHeight(event.nativeEvent.layout.height)}
        style={[
          popoverStyles.menu,
          {
            left: position.left,
            top: position.top,
            // Measured first, then placed, so it never jumps into view.
            opacity: height == null ? 0 : 1,
            backgroundColor: colors.bgSurface,
            borderColor: colors.border,
            ...shadow("float", colors.shadowColor),
          },
        ]}
      >
        {items.map((item) => {
          const ink = item.disabled ? colors.disabledText : item.destructive ? colors.dangerText : colors.textPrimary;
          return (
            <Pressable
              key={item.key}
              disabled={item.disabled}
              accessibilityRole="menuitem"
              accessibilityLabel={item.accessibilityLabel ?? item.label}
              accessibilityHint={item.detail}
              accessibilityState={{ disabled: Boolean(item.disabled) }}
              onPress={() => onChoose(item)}
              style={(state) => [
                popoverStyles.item,
                (state as { hovered?: boolean }).hovered && !item.disabled ? { backgroundColor: colors.surfaceSubtle } : null,
                state.pressed && { backgroundColor: colors.surfacePressed },
              ]}
            >
              <Icon name={item.icon} size={18} color={item.destructive && !item.disabled ? colors.dangerText : item.disabled ? colors.disabledText : colors.textSecondary} />
              <View style={styles.copy}>
                <AppText variant="compact" numberOfLines={1} style={{ color: ink }}>
                  {item.label}
                </AppText>
                {item.detail ? (
                  <AppText variant="caption" tone="tertiary" numberOfLines={1}>
                    {item.detail}
                  </AppText>
                ) : null}
              </View>
              {item.trailing ? <View style={styles.trailing}>{item.trailing}</View> : null}
            </Pressable>
          );
        })}
      </View>
    </Modal>
  );
}

const popoverStyles = StyleSheet.create({
  menu: {
    position: "absolute",
    width: MENU_POPOVER_WIDTH,
    padding: 4,
    borderRadius: Radii.sm,
    borderWidth: StyleSheet.hairlineWidth,
    ...ContinuousCorners,
  },
  item: {
    minHeight: 36,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: Radii.xs,
  },
});

const styles = StyleSheet.create({
  title: {
    paddingHorizontal: 6,
    paddingBottom: 10,
  },
  group: {
    borderRadius: Radii.xl,
    ...ContinuousCorners,
    overflow: "hidden",
  },
  item: {
    minHeight: Math.max(TouchTarget, 54),
    flexDirection: "row",
    alignItems: "center",
    gap: 14,
    paddingHorizontal: 16,
    paddingVertical: 10,
  },
  copy: {
    flex: 1,
    minWidth: 0,
  },
  trailing: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
});
