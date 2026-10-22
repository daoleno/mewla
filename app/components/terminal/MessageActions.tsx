import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { Platform, Pressable, StyleSheet, View, type DimensionValue, type ViewStyle } from "react-native";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Icon, type IconName } from "../icons/Icon";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import { CODE_BLOCK_COPIED_RESET_MS } from "./InterfaceMessageCodeBlockCopy";
import type { MessageMenuAnchor } from "./MessageMenuOverlay";
import { createSteadyPress, type SteadyTouch } from "./steadyPress";
import {
  isRetryableTurn,
  messageActionKinds,
  messageCopyText,
  type MessageActionEnvironment,
  type MessageActionKind,
  type MessageActionSubject,
  type ReplyTurn,
  type ReplyTurns,
} from "./messageActionsModel";
import {
  TimelineTextSelectableContext,
  type TimelineTextSelectableContextValue,
} from "./TimelineTextSelectableContext";

export interface MessageActionsValue {
  environment: MessageActionEnvironment;
  /** Every turn's final reply, for its Copy and Retry row. */
  replyTurns: ReplyTurns;
  /** The message whose menu is open, for its highlight. */
  activeId: string | null;
  /** Opens the menu next to a message (phone). */
  openMenu(subject: MessageActionSubject, anchor: MessageMenuAnchor): void;
  copy(subject: MessageActionSubject): void;
  select(subject: MessageActionSubject): void;
  edit(subject: MessageActionSubject): void;
  retry(turn: ReplyTurn): void;
}

export const MessageActionsContext = createContext<MessageActionsValue | null>(null);

export function useMessageActions() {
  return useContext(MessageActionsContext);
}

export function useCopyFeedback(text: string) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), CODE_BLOCK_COPIED_RESET_MS);
    return () => clearTimeout(timer);
  }, [copied]);
  const copy = useCallback(() => {
    void Clipboard.setStringAsync(text).then(() => setCopied(true));
  }, [text]);
  return { copied, copy };
}

const LONG_PRESS_MS = 350;
/** A reply button is the 44 dp the platforms ask of a touch target, with a little more above and below. */
const REPLY_BUTTON_SIZE = 44;
const REPLY_BUTTON_HIT_SLOP = { top: 4, bottom: 4, left: 0, right: 0 };
const HIGHLIGHT_RADIUS = 20;

/**
 * The pressed view's rect in the window. The list is inverted, and a measure
 * of a view inside it reports where the layout puts it, mirrored from where it
 * is drawn, so only its size is taken from the measure. Where it is comes from
 * the touch: the finger's window position less its position inside the view.
 */
function pressedRect(
  view: View | null,
  touch: { windowX: number; windowY: number; viewX: number; viewY: number },
): Promise<MessageMenuAnchor["rect"] | null> {
  return new Promise((resolve) => {
    if (!view) return resolve(null);
    view.measureInWindow((_x, _y, width, height) =>
      resolve(
        Number.isFinite(width) && Number.isFinite(height)
          ? { x: touch.windowX - touch.viewX, y: touch.windowY - touch.viewY, width, height }
          : null,
      ),
    );
  });
}

/**
 * Wraps a message of one's own so it answers to the message actions: a long
 * press opens the menu beside it on the phone (as in the ChatGPT app), a hover
 * row shows on desktop (whose right click and text selection stay the
 * browser's own).
 *
 * The press is gesture-handler's: it works at the native touch level, so it
 * does not depend on the text view under the finger handing the touch on. The
 * bubble's text is not natively selectable (that long press is the menu's);
 * Select Text in the menu shows it where selection works. Replies are not
 * wrapped at all: their text keeps the system's own selection.
 */
export function MessageActionSurface({
  subject,
  align,
  maxWidth,
  radii,
  chrome,
  children,
}: {
  subject: MessageActionSubject;
  align: "start" | "end";
  maxWidth?: DimensionValue;
  /** The bubble's corner radii, so the pressed highlight is its shape. */
  radii?: ViewStyle;
  chrome: TerminalThemeChrome;
  children: React.ReactNode;
}) {
  const actions = useMessageActions();
  const desktop = useDesktopWeb();
  const [hovered, setHovered] = useState(false);
  const parentSelectable = useContext(TimelineTextSelectableContext);
  const bubbleRef = useRef<View>(null);
  const kinds = useMemo(
    () => (actions ? messageActionKinds(subject, actions.environment) : []),
    [actions, subject],
  );
  const menuEnabled = !desktop && kinds.length > 0;
  // A message with a menu does not start the system's own text selection: that
  // long press belongs to the menu, whose Select Text selects. Without a menu
  // (still sending, previews) the text keeps the timeline's rules.
  const selectable = useMemo<TimelineTextSelectableContextValue>(
    () => (menuEnabled ? { selectable: false } : parentSelectable),
    [menuEnabled, parentSelectable],
  );
  const open = useCallback(
    async (event: { x: number; y: number; absoluteX: number; absoluteY: number }) => {
      if (!actions) return;
      const rect = await pressedRect(bubbleRef.current, {
        windowX: event.absoluteX,
        windowY: event.absoluteY,
        viewX: event.x,
        viewY: event.y,
      });
      if (!rect) return;
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      actions.openMenu(subject, { rect, touchY: event.absoluteY, align });
    },
    [actions, align, subject],
  );
  const longPress = useMemo(
    () =>
      Gesture.LongPress()
        .runOnJS(true)
        .enabled(menuEnabled)
        .minDuration(LONG_PRESS_MS)
        .maxDistance(10)
        .onStart((event) => void open(event)),
    [menuEnabled, open],
  );
  const content = (
    <TimelineTextSelectableContext.Provider value={selectable}>
      {children}
    </TimelineTextSelectableContext.Provider>
  );
  const inner = [align === "end" ? styles.end : styles.fill, maxWidth ? { maxWidth } : null];

  if (desktop) {
    const web = {
      onHoverIn: () => setHovered(true),
      onHoverOut: () => setHovered(false),
    } as object;
    return (
      <Pressable
        accessible={false}
        focusable={false}
        {...web}
        style={[styles.desktopWrap, align === "end" ? styles.desktopEnd : null]}
      >
        <View style={inner}>{content}</View>
        {actions && kinds.length > 0 ? (
          <MessageActionBar
            subject={subject}
            kinds={kinds}
            visible={hovered}
            align={align}
            chrome={chrome}
            onEdit={actions.edit}
          />
        ) : null}
      </Pressable>
    );
  }
  const active = actions?.activeId === subject.id;
  return (
    <GestureDetector gesture={longPress}>
      <View ref={bubbleRef} collapsable={false} style={inner}>
        {content}
        {active ? (
          <View
            pointerEvents="none"
            style={[styles.highlight, radii, { backgroundColor: chrome.text, opacity: 0.1 }]}
          />
        ) : null}
      </View>
    </GestureDetector>
  );
}

/**
 * The quiet icon row under a turn's final reply, as in ChatGPT: Copy, Retry
 * where the agent can run the prompt again, and on the phone a menu with
 * Select Text. Older replies have it too.
 */
export function ReplyActionRow({
  subject,
  chrome,
}: {
  subject: MessageActionSubject;
  chrome: TerminalThemeChrome;
}) {
  const actions = useMessageActions();
  const desktop = useDesktopWeb();
  const moreRef = useRef<View>(null);
  const { copied, copy } = useCopyFeedback(messageCopyText(subject));
  const turn = actions?.replyTurns.get(subject.id);
  if (!actions || !turn || subject.streaming) return null;
  const retryable = isRetryableTurn(turn, actions.environment);
  const kinds = messageActionKinds(subject, actions.environment);
  const more = !desktop && kinds.includes("select");
  return (
    <View style={styles.replyRow}>
      <RowButton
        icon={copied ? "check" : "copy"}
        label={copied ? "Copied" : "Copy"}
        color={copied ? chrome.accent : chrome.textMuted}
        pressedColor={chrome.surfaceMuted}
        onPress={copy}
      />
      {retryable ? (
        <RowButton
          icon="refresh"
          label="Retry"
          color={chrome.textMuted}
          pressedColor={chrome.surfaceMuted}
          onPress={() => {
            void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
            actions.retry(turn);
          }}
        />
      ) : null}
      {more ? (
        <View ref={moreRef} collapsable={false}>
          <RowButton
            icon="more-horizontal"
            label="More"
            color={chrome.textMuted}
            pressedColor={chrome.surfaceMuted}
            onPress={({ pageX, pageY, locationX, locationY }) => {
              void pressedRect(moreRef.current, {
                windowX: pageX,
                windowY: pageY,
                viewX: locationX,
                viewY: locationY,
              }).then((rect) => {
                if (!rect) return;
                void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                actions.openMenu(subject, { rect, touchY: rect.y + rect.height / 2, align: "start" });
              });
            }}
          />
        </View>
      ) : null}
    </View>
  );
}

function RowButton({
  icon,
  label,
  color,
  pressedColor,
  onPress,
}: {
  icon: IconName;
  label: string;
  color: string;
  pressedColor: string;
  onPress(touch: SteadyTouch): void;
}) {
  const [pressed, setPressed] = useState(false);
  const onPressRef = useRef(onPress);
  onPressRef.current = onPress;
  const press = useMemo(
    () => createSteadyPress({ onPress: (touch) => onPressRef.current(touch), onActiveChange: setPressed }),
    [],
  );
  // Not a Pressable: see steadyPress. The icon ignores touches so the press
  // lands on the button itself and locationX is relative to it.
  return (
    <View
      accessible
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityActions={[{ name: "activate" }]}
      onAccessibilityAction={(event) => {
        if (event.nativeEvent.actionName === "activate") {
          onPressRef.current({ pageX: 0, pageY: 0, locationX: 0, locationY: 0 });
        }
      }}
      hitSlop={REPLY_BUTTON_HIT_SLOP}
      onStartShouldSetResponder={() => true}
      onMoveShouldSetResponder={() => false}
      onResponderGrant={press.grant}
      onResponderMove={press.move}
      onResponderRelease={press.release}
      onResponderTerminate={press.terminate}
      onResponderTerminationRequest={press.canTerminate}
      style={[styles.replyButton, pressed ? { backgroundColor: pressedColor } : null]}
      {...(Platform.OS === "web" ? ({ title: label } as object) : null)}
    >
      <View pointerEvents="none">
        <Icon name={icon} size={18} color={color} />
      </View>
    </View>
  );
}

/**
 * Desktop: a quiet icon row under the message, shown on hover or keyboard
 * focus. It floats in the gap below the message and takes no space of its own,
 * so a chat at rest is as compact as before and nothing moves when it appears.
 */
function MessageActionBar({
  subject,
  kinds,
  visible,
  align,
  chrome,
  onEdit,
}: {
  subject: MessageActionSubject;
  kinds: MessageActionKind[];
  visible: boolean;
  align: "start" | "end";
  chrome: TerminalThemeChrome;
  onEdit(subject: MessageActionSubject): void;
}) {
  const { copied, copy } = useCopyFeedback(messageCopyText(subject));
  const [focused, setFocused] = useState(false);
  const shown = visible || focused;
  return (
    <View
      style={[styles.bar, align === "end" ? styles.barEnd : null, { opacity: shown ? 1 : 0 }]}
      pointerEvents={shown ? "auto" : "none"}
    >
      <BarButton
        icon={copied ? "check" : "copy"}
        label={copied ? "Message copied" : "Copy message"}
        color={copied ? chrome.accent : chrome.textMuted}
        hover={chrome.surfaceMuted}
        onFocusChange={setFocused}
        onPress={copy}
      />
      {kinds.includes("edit") ? (
        <BarButton
          icon="edit"
          label="Edit message"
          color={chrome.textMuted}
          hover={chrome.surfaceMuted}
          onFocusChange={setFocused}
          onPress={() => onEdit(subject)}
        />
      ) : null}
    </View>
  );
}

function BarButton({
  icon,
  label,
  color,
  hover,
  onPress,
  onFocusChange,
}: {
  icon: IconName;
  label: string;
  color: string;
  hover: string;
  onPress(): void;
  onFocusChange(focused: boolean): void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      onFocus={() => onFocusChange(true)}
      onBlur={() => onFocusChange(false)}
      style={(state) => [
        styles.barButton,
        (state as { hovered?: boolean }).hovered ? { backgroundColor: hover } : null,
      ]}
      {...(Platform.OS === "web" ? ({ title: label } as object) : null)}
    >
      <Icon name={icon} size={15} color={color} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  fill: { alignSelf: "stretch", width: "100%" },
  end: { alignSelf: "flex-end" },
  barEnd: { justifyContent: "flex-end" },
  desktopWrap: { alignSelf: "stretch", width: "100%", minWidth: 0, position: "relative" },
  desktopEnd: { alignItems: "flex-end" },
  bar: {
    position: "absolute",
    top: "100%",
    left: 0,
    right: 0,
    zIndex: 2,
    flexDirection: "row",
    alignItems: "center",
    gap: 2,
    height: 26,
  },
  barButton: {
    width: 26,
    height: 26,
    borderRadius: 7,
    alignItems: "center",
    justifyContent: "center",
  },
  highlight: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    borderRadius: HIGHLIGHT_RADIUS,
  },
  replyRow: {
    flexDirection: "row",
    alignItems: "center",
    // The icons sit in 44 dp targets; the first icon still lines up with the text.
    marginLeft: -(REPLY_BUTTON_SIZE - 18) / 2 + 1,
  },
  replyButton: {
    width: REPLY_BUTTON_SIZE,
    height: REPLY_BUTTON_SIZE,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
  },
});
