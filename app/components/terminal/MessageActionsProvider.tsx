import React, { useCallback, useMemo, useState } from "react";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { ActionMenu } from "../ui/ActionMenu";
import { useOptionalToast } from "../ui/Toast";
import { MessageActionsContext, type MessageActionsValue } from "./MessageActions";
import { MessageTextSheet } from "./MessageTextSheet";
import {
  messageActionKinds,
  messageCopyText,
  messageMenuItems,
  type LatestReply,
  type MessageActionEnvironment,
  type MessageActionSubject,
} from "./messageActionsModel";

/**
 * Owns the message sheet (phone) and routes its choices, the hover row and the
 * reply row to the Session's copy, select, edit and retry handlers.
 */
export function MessageActionsProvider({
  environment,
  latestReply,
  onEdit,
  onRetry,
  children,
}: {
  environment: MessageActionEnvironment;
  latestReply: LatestReply | undefined;
  onEdit(subject: MessageActionSubject): void;
  onRetry(prompt: MessageActionSubject): void;
  children: React.ReactNode;
}) {
  const toast = useOptionalToast();
  // The subject outlives the sheet's close so the rows do not empty out.
  const [menu, setMenu] = useState<MessageActionSubject | null>(null);
  const [menuVisible, setMenuVisible] = useState(false);
  const [selectText, setSelectText] = useState<string | null>(null);
  const closeMenu = useCallback(() => setMenuVisible(false), []);
  const closeSelect = useCallback(() => setSelectText(null), []);

  const value = useMemo<MessageActionsValue>(
    () => ({
      environment,
      latestReply,
      openMenu: (subject) => {
        setMenu(subject);
        setMenuVisible(true);
      },
      copy: (subject) => {
        void Clipboard.setStringAsync(messageCopyText(subject)).then(() => {
          void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
          toast?.show({ title: "Copied", tone: "success" });
        });
      },
      select: (subject) => setSelectText(messageCopyText(subject)),
      edit: onEdit,
      retry: onRetry,
    }),
    [environment, latestReply, onEdit, onRetry, toast],
  );

  const items = useMemo(() => {
    if (!menu) return [];
    return messageMenuItems(messageActionKinds(menu, environment), (kind) => {
      if (kind === "copy") value.copy(menu);
      else if (kind === "select") value.select(menu);
      else value.edit(menu);
    });
  }, [environment, menu, value]);

  return (
    <MessageActionsContext.Provider value={value}>
      {children}
      <ActionMenu visible={menuVisible && menu !== null} items={items} onClose={closeMenu} />
      <MessageTextSheet text={selectText} onClose={closeSelect} />
    </MessageActionsContext.Provider>
  );
}
