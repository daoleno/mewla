import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { Radii, Typography, shadow, useAppColors } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import {
  filterPaletteItems,
  movePaletteSelection,
  type PaletteAction,
  type PaletteItem,
} from "./desktopPalette";
import { desktopShortcutRows, modKeyLabel } from "./desktopShortcuts";

function platformModLabel() {
  return modKeyLabel(typeof navigator === "undefined" ? undefined : navigator.platform);
}

/** Esc closes the open desktop overlay before anything else sees the key. */
function useEscapeToClose(visible: boolean, onClose: () => void) {
  useEffect(() => {
    if (!visible || typeof document === "undefined") return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      onClose();
    };
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [onClose, visible]);
}

/** Focus goes back where it was when an overlay closes. */
function useRestoreFocus(visible: boolean) {
  useEffect(() => {
    if (!visible || typeof document === "undefined") return;
    const previous = document.activeElement as HTMLElement | null;
    return () => previous?.focus?.();
  }, [visible]);
}

function DesktopOverlay({
  visible,
  label,
  width,
  onClose,
  children,
}: {
  visible: boolean;
  label: string;
  width: number;
  onClose(): void;
  children: React.ReactNode;
}) {
  const colors = useAppColors();
  useEscapeToClose(visible, onClose);
  useRestoreFocus(visible);
  if (!visible) return null;
  return (
    <View style={styles.overlay} role="dialog" aria-modal aria-label={label}>
      <Pressable
        accessibilityLabel="Close"
        style={[StyleSheet.absoluteFill, { backgroundColor: colors.modalBackdrop }]}
        onPress={onClose}
        tabIndex={-1}
      />
      <View
        style={[
          styles.card,
          shadow("float"),
          { width, backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle },
        ]}
      >
        {children}
      </View>
    </View>
  );
}

const SECTION_ORDER: PaletteItem["section"][] = ["Actions", "Go to", "Work", "Sessions"];

export function DesktopCommandPalette({
  visible,
  items,
  onRun,
  onClose,
}: {
  visible: boolean;
  items: readonly PaletteItem[];
  onRun(action: PaletteAction): void;
  onClose(): void;
}) {
  const colors = useAppColors();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const listRef = useRef<ScrollView>(null);
  useEffect(() => {
    if (visible) {
      setQuery("");
      setSelected(0);
    }
  }, [visible]);
  // Sections keep their order; the flat list drives the arrow keys.
  const ordered = useMemo(() => {
    const filtered = filterPaletteItems(items, query);
    if (query.trim()) return filtered;
    return SECTION_ORDER.flatMap((section) => filtered.filter((item) => item.section === section));
  }, [items, query]);
  const current = Math.min(selected, Math.max(0, ordered.length - 1));

  return (
    <DesktopOverlay visible={visible} label="Command palette" width={620} onClose={onClose}>
      <View style={[styles.searchRow, { borderBottomColor: colors.borderSubtle }]}>
        <Icon name="search" size={18} color={colors.textTertiary} />
        <TextInput
          autoFocus
          value={query}
          onChangeText={(value) => {
            setQuery(value);
            setSelected(0);
          }}
          placeholder="Go to, open a Session or Work, or run an action…"
          placeholderTextColor={colors.textTertiary}
          accessibilityLabel="Search commands"
          role="combobox"
          aria-expanded
          aria-controls="mewla-palette-list"
          aria-activedescendant={ordered[current] ? `palette-${current}` : undefined}
          style={[styles.searchInput, { color: colors.textPrimary }]}
          onKeyPress={(event) => {
            const native = event.nativeEvent as unknown as KeyboardEvent;
            if (native.isComposing || native.keyCode === 229) return;
            if (native.key === "ArrowDown" || native.key === "ArrowUp") {
              event.preventDefault();
              setSelected(movePaletteSelection(current, native.key === "ArrowDown" ? 1 : -1, ordered.length));
            } else if (native.key === "Enter") {
              event.preventDefault();
              const item = ordered[current];
              if (item) onRun(item.action);
            }
          }}
        />
      </View>
      <ScrollView
        ref={listRef}
        nativeID="mewla-palette-list"
        {...({ role: "listbox" } as object)}
        style={styles.list}
        contentContainerStyle={styles.listContent}
        keyboardShouldPersistTaps="handled"
      >
        {ordered.length === 0 ? (
          <Text style={[styles.empty, { color: colors.textTertiary }]}>Nothing matches “{query}”.</Text>
        ) : (
          ordered.map((item, index) => {
            const showSection = index === 0 || ordered[index - 1]?.section !== item.section;
            const active = index === current;
            return (
              <React.Fragment key={item.id}>
                {showSection && !query.trim() ? (
                  <Text style={[styles.section, { color: colors.textTertiary }]}>{item.section}</Text>
                ) : null}
                <Pressable
                  nativeID={`palette-${index}`}
                  role="option"
                  aria-selected={active}
                  onHoverIn={() => setSelected(index)}
                  onPress={() => onRun(item.action)}
                  tabIndex={-1}
                  ref={(node) => {
                    if (active && node) {
                      (node as unknown as HTMLElement).scrollIntoView?.({ block: "nearest" });
                    }
                  }}
                  style={[
                    styles.row,
                    { backgroundColor: active ? colors.surfaceActive : "transparent" },
                  ]}
                >
                  <Text numberOfLines={1} style={[styles.rowLabel, { color: colors.textPrimary }]}>
                    {item.label}
                  </Text>
                  {item.detail ? (
                    <Text numberOfLines={1} style={[styles.rowDetail, { color: colors.textTertiary }]}>
                      {query.trim() ? `${item.section} · ${item.detail}` : item.detail}
                    </Text>
                  ) : query.trim() ? (
                    <Text numberOfLines={1} style={[styles.rowDetail, { color: colors.textTertiary }]}>
                      {item.section}
                    </Text>
                  ) : null}
                  {item.shortcut ? (
                    <Text style={[styles.kbd, { color: colors.textSecondary, borderColor: colors.border }]}>
                      {item.shortcut}
                    </Text>
                  ) : null}
                </Pressable>
              </React.Fragment>
            );
          })
        )}
      </ScrollView>
      <View style={[styles.footer, { borderTopColor: colors.borderSubtle }]}>
        <Text style={[styles.footerText, { color: colors.textTertiary }]}>
          ↑↓ to move · Enter to open · Esc to close · {platformModLabel()}+K anywhere
        </Text>
      </View>
    </DesktopOverlay>
  );
}

export function DesktopShortcutsDialog({
  visible,
  onClose,
}: {
  visible: boolean;
  onClose(): void;
}) {
  const colors = useAppColors();
  const rows = useMemo(() => desktopShortcutRows(platformModLabel()), []);
  return (
    <DesktopOverlay visible={visible} label="Keyboard shortcuts" width={520} onClose={onClose}>
      <View style={[styles.dialogHeader, { borderBottomColor: colors.borderSubtle }]}>
        <Text accessibilityRole="header" style={[styles.dialogTitle, { color: colors.textPrimary }]}>
          Keyboard shortcuts
        </Text>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Close"
          onPress={onClose}
          style={(state) => [
            styles.closeButton,
            (state as { hovered?: boolean }).hovered ? { backgroundColor: colors.surfaceSubtle } : null,
          ]}
        >
          <Icon name="close" size={18} color={colors.textSecondary} />
        </Pressable>
      </View>
      <ScrollView style={styles.list} contentContainerStyle={styles.listContent}>
        {rows.map((row) => (
          <View key={row.label} style={styles.shortcutRow}>
            <Text style={[styles.rowLabel, { color: colors.textPrimary }]}>{row.label}</Text>
            <Text style={[styles.kbd, { color: colors.textSecondary, borderColor: colors.border }]}>
              {row.keys}
            </Text>
          </View>
        ))}
      </ScrollView>
    </DesktopOverlay>
  );
}

const styles = StyleSheet.create({
  overlay: {
    ...StyleSheet.absoluteFill,
    zIndex: 1000,
    alignItems: "center",
    paddingTop: "12%",
    paddingHorizontal: 16,
  },
  card: {
    maxWidth: "100%",
    maxHeight: "70%",
    borderRadius: Radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  searchRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  searchInput: {
    flex: 1,
    height: 52,
    fontFamily: Typography.uiFont,
    fontSize: 16,
    outlineStyle: "none",
  } as object,
  list: {
    flexGrow: 0,
  },
  listContent: {
    padding: 6,
  },
  section: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 11,
    lineHeight: 15,
    paddingHorizontal: 12,
    paddingTop: 10,
    paddingBottom: 4,
  },
  row: {
    minHeight: 38,
    paddingHorizontal: 12,
    borderRadius: Radii.xs,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  rowLabel: {
    flexShrink: 1,
    fontFamily: Typography.uiFont,
    fontSize: 14,
    lineHeight: 20,
  },
  rowDetail: {
    flex: 1,
    minWidth: 0,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    lineHeight: 17,
  },
  kbd: {
    marginLeft: "auto",
    fontFamily: Typography.terminalFont,
    fontSize: 11,
    lineHeight: 16,
    paddingHorizontal: 6,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 5,
  },
  empty: {
    padding: 16,
    fontFamily: Typography.uiFont,
    fontSize: 14,
  },
  footer: {
    paddingHorizontal: 16,
    paddingVertical: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  footerText: {
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
  dialogHeader: {
    flexDirection: "row",
    alignItems: "center",
    paddingLeft: 18,
    paddingRight: 8,
    minHeight: 52,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  dialogTitle: {
    flex: 1,
    fontFamily: Typography.displayFontSemibold,
    fontSize: 18,
  },
  closeButton: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: "center",
    justifyContent: "center",
  },
  shortcutRow: {
    minHeight: 34,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
});
