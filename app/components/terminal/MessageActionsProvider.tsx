import React, { useCallback, useMemo, useRef, useState } from "react";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ActionMenu, type ActionMenuItem } from "../ui/ActionMenu";
import { useOptionalToast } from "../ui/Toast";
import { MessageActionsContext, type MessageActionsValue } from "./MessageActions";
import { MessageEditCue } from "./MessageEditChip";
import {
  messageActionKinds,
  messageCopyText,
  messageEditMode,
  type MessageActionEnvironment,
  type MessageActionSubject,
} from "./messageActionsModel";

/**
 * Owns the message menu (phone) and routes menu and hover-row choices to the
 * Session's edit and resend handlers.
 */
export function MessageActionsProvider({
  environment,
  chrome,
  onEdit,
  onResend,
  children,
}: {
  environment: MessageActionEnvironment;
  chrome: TerminalThemeChrome;
  onEdit(subject: MessageActionSubject): void;
  onResend(subject: MessageActionSubject): void;
  children: React.ReactNode;
}) {
  const toast = useOptionalToast();
  // The subject outlives the sheet's close animation so it does not empty out.
  const [menu, setMenu] = useState<MessageActionSubject | null>(null);
  const [menuVisible, setMenuVisible] = useState(false);
  const [selectingId, setSelectingId] = useState<string | null>(null);
  const environmentRef = useRef(environment);
  environmentRef.current = environment;

  const value = useMemo<MessageActionsValue>(
    () => ({
      environment,
      selectingId,
      openMenu: (subject) => {
        setSelectingId(null);
        setMenu(subject);
        setMenuVisible(true);
      },
      run: (kind, subject) => {
        if (kind === "edit") onEdit(subject);
        else onResend(subject);
      },
    }),
    [environment, onEdit, onResend, selectingId],
  );

  const items = useMemo<ActionMenuItem[]>(() => {
    if (!menu) return [];
    const mode = messageEditMode(environment);
    return messageActionKinds(menu, environment).map((kind): ActionMenuItem => {
      switch (kind) {
        case "copy":
          return {
            key: kind,
            label: "Copy",
            icon: "copy",
            onPress: () => {
              void Clipboard.setStringAsync(messageCopyText(menu)).then(() => {
                void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
                toast?.show({ title: "Copied", tone: "success" });
              });
            },
          };
        case "select":
          return { key: kind, label: "Select text", icon: "text", onPress: () => setSelectingId(menu.id) };
        case "edit":
          return {
            key: kind,
            label: "Edit",
            icon: "edit",
            trailing: <MessageEditCue mode={mode} chrome={chrome} />,
            onPress: () => onEdit(menu),
          };
        default:
          return { key: kind, label: "Resend", icon: "refresh", onPress: () => onResend(menu) };
      }
    });
  }, [chrome, environment, menu, onEdit, onResend, toast]);

  const close = useCallback(() => setMenuVisible(false), []);
  return (
    <MessageActionsContext.Provider value={value}>
      {children}
      <ActionMenu visible={menuVisible && menu !== null} items={items} onClose={close} />
    </MessageActionsContext.Provider>
  );
}
