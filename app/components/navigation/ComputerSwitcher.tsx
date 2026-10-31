import React, { useEffect, useRef } from "react";
import { Platform, Pressable, StyleSheet, Text, View, type ViewInstance } from "react-native";
import { Typography, useAppTheme } from "../../constants/tokens";
import { Icon, type IconName } from "../icons/Icon";
import { ActionMenu } from "../ui/ActionMenu";
import { GlassSurface } from "../ui/GlassSurface";
import {
  computerRowLabel,
  type ComputerSwitcherRow,
  type ComputerTone,
} from "./computerSwitcherModel";

interface ComputerSwitcherProps {
  visible: boolean;
  rows: readonly ComputerSwitcherRow[];
  /** The "Add / manage" entry: Settings → Computers. */
  manageIcon: IconName;
  /** Desktop web docks the panel: the switcher is a popover above the row. */
  docked: boolean;
  onSelect(id: string): void;
  onManage(): void;
  onClose(): void;
}

/** Quiet states have no ink of their own; only offline or an issue is colored. */
export function useToneInk(): (tone: ComputerTone) => string | null {
  const { colors } = useAppTheme();
  return (tone) => (tone === "issue" ? colors.dangerText : tone === "offline" ? colors.warning : null);
}

/**
 * Every paired computer, the one in use marked, and a way to add or manage.
 * One list for both layouts: a sheet on the phone, a popover on desktop web.
 */
export function ComputerSwitcher(props: ComputerSwitcherProps) {
  return props.docked ? <ComputerPopover {...props} /> : <ComputerSheet {...props} />;
}

function ComputerSheet({ visible, rows, manageIcon, onSelect, onManage, onClose }: ComputerSwitcherProps) {
  const { colors } = useAppTheme();
  const toneInk = useToneInk();
  return (
    <ActionMenu
      visible={visible}
      title="Computers"
      onClose={onClose}
      items={[
        ...rows.map((row) => ({
          key: row.id,
          label: row.name,
          icon: "desktop" as const,
          accessibilityLabel: computerRowLabel(row),
          trailing: (
            <>
              {row.status ? (
                <Text style={[styles.status, { color: toneInk(row.tone) ?? colors.textTertiary }]}>{row.status}</Text>
              ) : null}
              {row.current ? <Icon name="check" size={18} color={colors.textPrimary} /> : null}
            </>
          ),
          onPress: () => {
            if (!row.current) onSelect(row.id);
          },
        })),
        { key: "manage", label: "Add or manage", icon: manageIcon, onPress: onManage },
      ]}
    />
  );
}

function ComputerPopover({ visible, rows, manageIcon, onSelect, onManage, onClose }: ComputerSwitcherProps) {
  const { colors } = useAppTheme();
  const toneInk = useToneInk();
  const popoverRef = useRef<ViewInstance>(null);

  // Web only: Esc and a click outside close it, and focus starts on the list
  // so the keyboard reaches every row; focus goes back to the row on close.
  useEffect(() => {
    if (!visible || Platform.OS !== "web") return;
    const popover = popoverRef.current as unknown as HTMLElement | null;
    const opener = document.activeElement as HTMLElement | null;
    const items = () => Array.from(popover?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? []);
    (items().find((item) => item.getAttribute("aria-checked") === "true") ?? items()[0])?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        onClose();
        return;
      }
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
      const list = items();
      if (list.length === 0) return;
      event.preventDefault();
      const at = list.indexOf(document.activeElement as HTMLElement);
      const next = event.key === "ArrowDown" ? (at + 1) % list.length : (at <= 0 ? list.length - 1 : at - 1);
      list[next].focus();
    };
    const onPointer = (event: PointerEvent) => {
      const target = event.target as Node | null;
      // The row that opened it toggles it; a press there is not "outside".
      if (target && !popover?.contains(target) && !opener?.contains(target)) onClose();
    };
    document.addEventListener("keydown", onKey, true);
    document.addEventListener("pointerdown", onPointer, true);
    return () => {
      document.removeEventListener("keydown", onKey, true);
      document.removeEventListener("pointerdown", onPointer, true);
      opener?.focus?.();
    };
  }, [visible, onClose]);

  if (!visible) return null;
  const pick = (action: () => void) => () => {
    onClose();
    action();
  };
  return (
    <View ref={popoverRef} style={styles.popover}>
      <GlassSurface elevation="float" radius={12}>
        <View role="menu" aria-label="Computers" style={styles.popoverList}>
          {rows.map((row) => (
            <Pressable
              key={row.id}
              role="menuitem"
              aria-checked={row.current}
              accessibilityLabel={computerRowLabel(row)}
              onPress={pick(() => {
                if (!row.current) onSelect(row.id);
              })}
              style={(state) => [
                styles.popoverItem,
                {
                  backgroundColor: state.pressed
                    ? colors.surfacePressed
                    : (state as { hovered?: boolean }).hovered
                      ? colors.surfaceSubtle
                      : "transparent",
                },
              ]}
            >
              <Icon name="desktop" size={16} color={colors.textSecondary} />
              <Text numberOfLines={1} style={[styles.popoverName, { color: colors.textPrimary }]}>
                {row.name}
              </Text>
              {row.status ? (
                <Text numberOfLines={1} style={[styles.status, { color: toneInk(row.tone) ?? colors.textTertiary }]}>
                  {row.status}
                </Text>
              ) : null}
              {row.current ? <Icon name="check" size={16} color={colors.textPrimary} /> : <View style={styles.checkSlot} />}
            </Pressable>
          ))}
          <View style={[styles.divider, { backgroundColor: colors.borderSubtle }]} />
          <Pressable
            role="menuitem"
            accessibilityLabel="Add or manage"
            onPress={pick(onManage)}
            style={(state) => [
              styles.popoverItem,
              {
                backgroundColor: state.pressed
                  ? colors.surfacePressed
                  : (state as { hovered?: boolean }).hovered
                    ? colors.surfaceSubtle
                    : "transparent",
              },
            ]}
          >
            <Icon name={manageIcon} size={16} color={colors.textSecondary} />
            <Text numberOfLines={1} style={[styles.popoverName, { color: colors.textSecondary }]}>
              Add or manage
            </Text>
          </Pressable>
        </View>
      </GlassSurface>
    </View>
  );
}

const styles = StyleSheet.create({
  status: {
    fontSize: 12.5,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  popover: {
    position: "absolute",
    left: 0,
    right: 0,
    bottom: "100%",
    marginBottom: 6,
    zIndex: 10,
  },
  popoverList: {
    padding: 4,
  },
  popoverItem: {
    minHeight: 40,
    paddingHorizontal: 10,
    borderRadius: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  popoverName: {
    flex: 1,
    minWidth: 0,
    fontSize: 14,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
  },
  checkSlot: {
    width: 16,
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    marginVertical: 4,
    marginHorizontal: 6,
  },
});
