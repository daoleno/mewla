import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Animated,
  BackHandler,
  Easing,
  Keyboard,
  Platform,
  Pressable,
  StyleSheet,
  useWindowDimensions,
  View, type ViewInstance,
} from "react-native";
import * as Haptics from "expo-haptics";
import { Radii, shadow, useAppTheme } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import { AppText } from "../ui/AppText";
import type { MessageActionKind, MessageMenuEntry } from "./messageActionsModel";
import { placeMessageMenu, type MenuRect } from "./messageMenuPlacement";

/** Where a message sits in the window when it was pressed. */
export interface MessageMenuAnchor {
  rect: MenuRect;
  touchY: number;
  align: "start" | "end";
}

export interface MessageMenuState {
  anchor: MessageMenuAnchor;
  entries: MessageMenuEntry[];
  /** A message of one's own says when it was sent. */
  heading: string;
}

const MENU_WIDTH = 224;
const ROW_HEIGHT = 48;
const HEADING_HEIGHT = 40;
const OPEN_MS = 140;

function useKeyboardTop() {
  const [height, setHeight] = useState(0);
  useEffect(() => {
    const show = Keyboard.addListener(
      Platform.OS === "ios" ? "keyboardWillShow" : "keyboardDidShow",
      (event) => setHeight(event.endCoordinates.height),
    );
    const hide = Keyboard.addListener(
      Platform.OS === "ios" ? "keyboardWillHide" : "keyboardDidHide",
      () => setHeight(0),
    );
    return () => {
      show.remove();
      hide.remove();
    };
  }, []);
  return height;
}

/**
 * The menu that follows a pressed message, as in the ChatGPT app: a compact
 * popover next to the message, over a conversation that stays visible. Any
 * touch outside it, the keyboard moving or Back closes it.
 *
 * It fills the chat surface and measures its own place in the window, so the
 * pressed message's window rect (which already accounts for the inverted
 * list) and the keyboard's top land in the same space.
 */
export function MessageMenuOverlay({
  menu,
  onChoose,
  onClose,
}: {
  menu: MessageMenuState | null;
  onChoose(kind: MessageActionKind): void;
  onClose(): void;
}) {
  const { colors, theme } = useAppTheme();
  const rootRef = useRef<ViewInstance>(null);
  const [frame, setFrame] = useState<{ x: number; y: number; width: number; height: number } | null>(null);
  const { height: windowHeight } = useWindowDimensions();
  const keyboardHeight = useKeyboardTop();
  const progress = useRef(new Animated.Value(0)).current;
  const open = menu !== null;

  // The keyboard moving under an open menu would leave it beside nothing.
  const lastKeyboardHeight = useRef(keyboardHeight);
  useEffect(() => {
    if (lastKeyboardHeight.current === keyboardHeight) return;
    lastKeyboardHeight.current = keyboardHeight;
    if (open) onClose();
  }, [keyboardHeight, onClose, open]);

  useEffect(() => {
    if (!open) {
      setFrame(null);
      progress.setValue(0);
      return;
    }
    const subscription = BackHandler.addEventListener("hardwareBackPress", () => {
      onClose();
      return true;
    });
    return () => subscription.remove();
  }, [onClose, open, progress]);

  const measure = useCallback(() => {
    rootRef.current?.measureInWindow((x, y, width, height) => setFrame({ x, y, width, height }));
  }, []);
  useEffect(() => {
    if (open) measure();
  }, [measure, open]);

  const menuHeight = menu ? menu.entries.length * ROW_HEIGHT + (menu.heading ? HEADING_HEIGHT : 0) + 8 : 0;
  const placement = useMemo(() => {
    if (!menu || !frame) return null;
    return placeMessageMenu({
      anchor: {
        x: menu.anchor.rect.x - frame.x,
        y: menu.anchor.rect.y - frame.y,
        width: menu.anchor.rect.width,
        height: menu.anchor.rect.height,
      },
      touchY: menu.anchor.touchY - frame.y,
      menu: { width: MENU_WIDTH, height: menuHeight },
      bounds: {
        top: 0,
        bottom: Math.min(frame.height, windowHeight - keyboardHeight - frame.y),
        left: 0,
        right: frame.width,
      },
      align: menu.anchor.align,
    });
  }, [frame, keyboardHeight, menu, menuHeight, windowHeight]);

  useEffect(() => {
    if (!placement) return;
    progress.setValue(0);
    Animated.timing(progress, {
      toValue: 1,
      duration: OPEN_MS,
      easing: Easing.out(Easing.cubic),
      useNativeDriver: true,
    }).start();
    // Once per opening: moving the menu later must not replay the animation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [placement !== null, menu]);

  if (!menu) return null;
  return (
    <View
      ref={rootRef}
      collapsable={false}
      style={StyleSheet.absoluteFill}
      onLayout={measure}
      // Takes the touch that closes it, so the tap does not also land on the chat.
      onStartShouldSetResponder={() => true}
      onResponderGrant={onClose}
      testID="message-menu-overlay"
    >
      {placement ? (
        <Animated.View
          // The menu's own touches must not reach the closing overlay.
          onStartShouldSetResponder={() => true}
          style={[
            styles.menu,
            shadow("float", colors.shadowColor),
            {
              left: placement.left,
              top: placement.top,
              width: MENU_WIDTH,
              backgroundColor: colors.bgSurface,
              borderColor: theme.materials.separator,
              opacity: progress,
              transform: [{ scale: progress.interpolate({ inputRange: [0, 1], outputRange: [0.86, 1] }) }],
              transformOrigin: `${placement.origin.x}px ${placement.origin.y}px`,
            },
          ]}
        >
          {menu.heading ? (
            <View style={[styles.heading, { borderBottomColor: theme.materials.separator }]}>
              <AppText variant="caption" tone="tertiary" numberOfLines={1}>
                {menu.heading}
              </AppText>
            </View>
          ) : null}
          {menu.entries.map((entry) => (
            <Pressable
              key={entry.kind}
              accessibilityRole="button"
              accessibilityLabel={entry.label}
              android_ripple={{ color: colors.surfacePressed }}
              onPress={() => {
                void Haptics.selectionAsync();
                onChoose(entry.kind);
              }}
              style={({ pressed }) => [styles.row, pressed && { backgroundColor: colors.surfacePressed }]}
            >
              <AppText variant="body" style={styles.label}>
                {entry.label}
              </AppText>
              <Icon name={entry.icon} size={20} color={colors.textSecondary} />
            </Pressable>
          ))}
        </Animated.View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  menu: {
    position: "absolute",
    borderRadius: Radii.xl,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    paddingBottom: 4,
  },
  heading: {
    height: HEADING_HEIGHT,
    justifyContent: "center",
    paddingHorizontal: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
    marginBottom: 4,
  },
  row: {
    height: ROW_HEIGHT,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    paddingHorizontal: 16,
  },
  label: { flex: 1 },
});
