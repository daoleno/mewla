import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { Platform, Pressable, StyleSheet, View, type DimensionValue } from "react-native";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Icon, type IconName } from "../icons/Icon";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import { CODE_BLOCK_COPIED_RESET_MS } from "./InterfaceMessageCodeBlockCopy";
import {
  isRerunnablePrompt,
  messageActionKinds,
  messageCopyText,
  type LatestReply,
  type MessageActionEnvironment,
  type MessageActionKind,
  type MessageActionSubject,
} from "./messageActionsModel";
import {
  TimelineTextSelectableContext,
  type TimelineTextSelectableContextValue,
} from "./TimelineTextSelectableContext";

export interface MessageActionsValue {
  environment: MessageActionEnvironment;
  /** The newest reply and its prompt, for the reply's Copy and Retry row. */
  latestReply: LatestReply | undefined;
  /** Opens the message menu (phone). */
  openMenu(subject: MessageActionSubject): void;
  copy(subject: MessageActionSubject): void;
  select(subject: MessageActionSubject): void;
  edit(subject: MessageActionSubject): void;
  retry(prompt: MessageActionSubject): void;
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
// Two recognizers watch the same press; the second to fire is the same press.
const SAME_PRESS_MS = 800;

/**
 * Wraps one message so it answers to the message actions: a long press opens
 * the message sheet on the phone (as in the ChatGPT and Claude apps), a hover
 * row shows on desktop (whose right click and text selection stay the
 * browser's own).
 *
 * The long press is watched twice on purpose. A gesture-handler recognizer
 * works at the native touch level, so it does not depend on the text view
 * under the finger handing the touch on to the JS responder; the Pressable is
 * the JS-responder path. Either opens the sheet, once.
 */
export function MessageActionSurface({
  subject,
  align,
  maxWidth,
  chrome,
  children,
}: {
  subject: MessageActionSubject;
  align: "start" | "end";
  maxWidth?: DimensionValue;
  chrome: TerminalThemeChrome;
  children: React.ReactNode;
}) {
  const actions = useMessageActions();
  const desktop = useDesktopWeb();
  const [hovered, setHovered] = useState(false);
  const parentSelectable = useContext(TimelineTextSelectableContext);
  const kinds = useMemo(
    () => (actions ? messageActionKinds(subject, actions.environment) : []),
    [actions, subject],
  );
  const menuEnabled = !desktop && kinds.length > 0;
  // A message with a menu does not start the system's own text selection: that
  // long press belongs to the sheet, whose Select Text selects. Without a menu
  // (streaming, previews) the text keeps the timeline's rules.
  const selectable = useMemo<TimelineTextSelectableContextValue>(
    () => (menuEnabled ? { selectable: false } : parentSelectable),
    [menuEnabled, parentSelectable],
  );
  const lastOpened = useRef(0);
  const open = useCallback(() => {
    if (!actions) return;
    const now = Date.now();
    if (now - lastOpened.current < SAME_PRESS_MS) return;
    lastOpened.current = now;
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    actions.openMenu(subject);
  }, [actions, subject]);
  const longPress = useMemo(
    () =>
      Gesture.LongPress()
        .runOnJS(true)
        .enabled(menuEnabled)
        .minDuration(LONG_PRESS_MS)
        .maxDistance(10)
        .onStart(open),
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
    // The newest reply carries its own always-visible Copy and Retry row.
    const rowOwner = actions?.latestReply?.replyId === subject.id;
    return (
      <Pressable
        accessible={false}
        focusable={false}
        {...web}
        style={[styles.desktopWrap, align === "end" ? styles.desktopEnd : null]}
      >
        <View style={inner}>{content}</View>
        {actions && kinds.length > 0 && !rowOwner ? (
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
  return (
    <GestureDetector gesture={longPress}>
      <Pressable
        accessible={false}
        focusable={false}
        delayLongPress={LONG_PRESS_MS}
        onLongPress={menuEnabled ? open : undefined}
        style={inner}
      >
        {content}
      </Pressable>
    </GestureDetector>
  );
}

/**
 * The newest reply's quiet icon row, always visible as in ChatGPT: Copy, and
 * Retry where the agent can run the prompt again.
 */
export function ReplyActionRow({
  subject,
  chrome,
}: {
  subject: MessageActionSubject;
  chrome: TerminalThemeChrome;
}) {
  const actions = useMessageActions();
  const { copied, copy } = useCopyFeedback(messageCopyText(subject));
  const latest = actions?.latestReply;
  if (!actions || !latest || latest.replyId !== subject.id || subject.streaming) return null;
  const retryable = isRerunnablePrompt(latest.prompt, actions.environment);
  return (
    <View style={styles.replyRow}>
      <RowButton
        icon={copied ? "check" : "copy"}
        label={copied ? "Copied" : "Copy"}
        color={copied ? chrome.accent : chrome.textMuted}
        onPress={copy}
      />
      {retryable ? (
        <RowButton
          icon="refresh"
          label="Retry"
          color={chrome.textMuted}
          onPress={() => actions.retry(latest.prompt)}
        />
      ) : null}
    </View>
  );
}

function RowButton({
  icon,
  label,
  color,
  onPress,
}: {
  icon: IconName;
  label: string;
  color: string;
  onPress(): void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      hitSlop={6}
      onPress={onPress}
      style={styles.replyButton}
      {...(Platform.OS === "web" ? ({ title: label } as object) : null)}
    >
      <Icon name={icon} size={18} color={color} />
    </Pressable>
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
  replyRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    marginTop: 6,
    marginLeft: -8,
  },
  replyButton: {
    width: 36,
    height: 32,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
});
