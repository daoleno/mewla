import React, { useCallback, useMemo, useState } from "react";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { useOptionalToast } from "../ui/Toast";
import { MessageActionsContext, type MessageActionsValue } from "./MessageActions";
import { MessageMenuOverlay, type MessageMenuAnchor } from "./MessageMenuOverlay";
import { MessageTextSheet } from "./MessageTextSheet";
import {
  messageActionKinds,
  messageCopyText,
  messageMenuEntries,
  type MessageActionEnvironment,
  type MessageActionSubject,
  type ReplyIds,
} from "./messageActionsModel";

function sentAt(timestamp: string | undefined) {
  const date = timestamp ? new Date(timestamp) : null;
  if (!date || !Number.isFinite(date.getTime())) return "";
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/**
 * Owns the message menu (phone) and routes its choices, the hover row and the
 * reply row to the Session's copy, select and edit handlers.
 */
export function MessageActionsProvider({
  environment,
  replyIds,
  onEdit,
  children,
}: {
  environment: MessageActionEnvironment;
  replyIds: ReplyIds;
  onEdit(subject: MessageActionSubject): void;
  children: React.ReactNode;
}) {
  const toast = useOptionalToast();
  const [menu, setMenu] = useState<{ subject: MessageActionSubject; anchor: MessageMenuAnchor } | null>(null);
  const [selectText, setSelectText] = useState<string | null>(null);
  const closeMenu = useCallback(() => setMenu(null), []);
  const closeSelect = useCallback(() => setSelectText(null), []);

  const value = useMemo<MessageActionsValue>(
    () => ({
      environment,
      replyIds,
      activeId: menu?.subject.id ?? null,
      openMenu: (subject, anchor) => setMenu({ subject, anchor }),
      copy: (subject) => {
        void Clipboard.setStringAsync(messageCopyText(subject)).then(() => {
          void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
          toast?.show({ title: "Copied", tone: "success" });
        });
      },
      select: (subject) => setSelectText(messageCopyText(subject)),
      edit: onEdit,
    }),
    [environment, menu, onEdit, replyIds, toast],
  );

  const entries = useMemo(
    () => (menu ? messageMenuEntries(messageActionKinds(menu.subject, environment)) : []),
    [environment, menu],
  );
  const overlay = useMemo(
    () =>
      menu
        ? {
            anchor: menu.anchor,
            entries,
            heading: menu.subject.role === "user" ? sentAt(menu.subject.timestamp) : "",
          }
        : null,
    [entries, menu],
  );
  const choose = useCallback(
    (kind: "copy" | "select" | "edit") => {
      if (!menu) return;
      const subject = menu.subject;
      setMenu(null);
      if (kind === "copy") value.copy(subject);
      else if (kind === "select") value.select(subject);
      else value.edit(subject);
    },
    [menu, value],
  );

  return (
    <MessageActionsContext.Provider value={value}>
      {children}
      <MessageMenuOverlay menu={overlay} onChoose={choose} onClose={closeMenu} />
      <MessageTextSheet text={selectText} onClose={closeSelect} />
    </MessageActionsContext.Provider>
  );
}
