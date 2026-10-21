import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { Platform, Pressable, StyleSheet, View } from "react-native";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Icon, type IconName } from "../icons/Icon";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import { CODE_BLOCK_COPIED_RESET_MS } from "./InterfaceMessageCodeBlockCopy";
import {
  messageActionKinds,
  messageCopyText,
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
  /** The message whose text is selectable (phone: after "Select text"). */
  selectingId: string | null;
  openMenu(subject: MessageActionSubject): void;
  run(kind: Exclude<MessageActionKind, "copy" | "select">, subject: MessageActionSubject): void;
}

export const MessageActionsContext = createContext<MessageActionsValue | null>(null);

export function useMessageActions() {
  return useContext(MessageActionsContext);
}

const LONG_PRESS_MS = 360;
const RESEND_ARMED_MS = 3000;

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

/**
 * Wraps one message so it answers to the message menu: a long press on the
 * phone, a context menu and a hover row on desktop. Bubble text is not
 * natively selectable on the phone (that long press belongs to the menu); the
 * menu's Select text turns selection on for that one message.
 */
export function MessageActionSurface({
  subject,
  align,
  chrome,
  children,
}: {
  subject: MessageActionSubject;
  align: "start" | "end";
  chrome: TerminalThemeChrome;
  children: React.ReactNode;
}) {
  const actions = useMessageActions();
  const desktop = useDesktopWeb();
  const [hovered, setHovered] = useState(false);
  const parentSelectable = useContext(TimelineTextSelectableContext);
  // On the phone a bubble's text is not natively selectable: a Text with its
  // own long-press handler would take the press from this wrapper, so those
  // handlers are dropped too. Select text hands both back for that one message.
  // Without the menu provider (previews, fixtures) text keeps its own rules.
  const selecting = actions?.selectingId === subject.id;
  const selectable = useMemo<TimelineTextSelectableContextValue>(
    () =>
      desktop || !actions
        ? parentSelectable
        : selecting
          ? { ...parentSelectable, selectable: true }
          : { selectable: false },
    [actions, desktop, parentSelectable, selecting],
  );
  const kinds = actions ? messageActionKinds(subject, actions.environment) : [];
  const enabled = actions !== null && kinds.length > 0;
  // The wrapper stays the same element whether or not there is anything to
  // offer, so a reply that finishes streaming does not remount its text.
  if (desktop) {
    const web = {
      onHoverIn: () => setHovered(true),
      onHoverOut: () => setHovered(false),
      onContextMenu: (event: { preventDefault?: () => void }) => {
        if (!actions || !enabled) return;
        event.preventDefault?.();
        actions.openMenu(subject);
      },
    } as object;
    return (
      <Pressable
        accessible={false}
        focusable={false}
        {...web}
        style={[styles.desktopWrap, align === "end" ? styles.desktopEnd : null]}
      >
        {children}
        {actions && enabled ? (
          <MessageActionBar
            subject={subject}
            kinds={kinds}
            visible={hovered}
            align={align}
            chrome={chrome}
            onRun={actions.run}
          />
        ) : null}
      </Pressable>
    );
  }
  return (
    <Pressable
      accessible={false}
      focusable={false}
      delayLongPress={LONG_PRESS_MS}
      onLongPress={
        actions && enabled && actions.selectingId !== subject.id
          ? () => {
              void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
              actions.openMenu(subject);
            }
          : undefined
      }
      style={align === "end" ? styles.phoneEnd : styles.fill}
    >
      <TimelineTextSelectableContext.Provider value={selectable}>
        {children}
      </TimelineTextSelectableContext.Provider>
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
  onRun,
}: {
  subject: MessageActionSubject;
  kinds: MessageActionKind[];
  visible: boolean;
  align: "start" | "end";
  chrome: TerminalThemeChrome;
  onRun: MessageActionsValue["run"];
}) {
  const { copied, copy } = useCopyFeedback(messageCopyText(subject));
  const [resendArmed, setResendArmed] = useState(false);
  const [focused, setFocused] = useState(false);
  useEffect(() => {
    if (!resendArmed) return;
    const timer = setTimeout(() => setResendArmed(false), RESEND_ARMED_MS);
    return () => clearTimeout(timer);
  }, [resendArmed]);
  const shown = visible || focused || resendArmed;
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
          onPress={() => onRun("edit", subject)}
        />
      ) : null}
      {kinds.includes("resend") ? (
        // One click arms it, a second sends: a stray click cannot send twice.
        <BarButton
          icon={resendArmed ? "paper-plane" : "refresh"}
          label={resendArmed ? "Confirm resend" : "Resend message"}
          color={resendArmed ? chrome.accent : chrome.textMuted}
          hover={chrome.surfaceMuted}
          onFocusChange={setFocused}
          onPress={() => {
            if (!resendArmed) {
              setResendArmed(true);
              return;
            }
            setResendArmed(false);
            onRun("resend", subject);
          }}
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
  alignEnd: { alignSelf: "flex-end", alignItems: "flex-end" },
  barEnd: { justifyContent: "flex-end" },
  phoneEnd: { alignSelf: "stretch", width: "100%", flexDirection: "row", justifyContent: "flex-end" },
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
});
