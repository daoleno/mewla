import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { wsClient } from "../../services/websocket";
import { MessageEditChip } from "./MessageEditChip";
import {
  createResendGuard,
  editIsSendable,
  messageEditMode,
  type MessageActionEnvironment,
  type MessageActionSubject,
  type MessageEdit,
  type ReplyTurns,
} from "./messageActionsModel";

interface Input {
  serverId: string;
  workerId: string;
  conversationScopeKey?: string;
  connected: boolean;
  running: boolean;
  /** Daemon capability: this Session's CLI can rewind. */
  messageRewind: boolean;
  replyTurns: ReplyTurns;
  desktop: boolean;
  chrome: TerminalThemeChrome;
  draft: string;
  setDraft(value: string): void;
  focusComposer(): void;
  sendDraft(): void;
  resendMessage(text: string): void;
}

const INPUT_NOT_CLEAN =
  "Rewound, but the agent's input box still has text. Clear it in the Terminal, then send.";

/**
 * Edit and retry for one Interface chat.
 *
 * Edit puts the message in the composer. Sending it either rewinds the agent's
 * conversation to that message first (where the CLI can), or goes out as a new
 * message. Retry is the same without the editing: it sends a reply's prompt
 * again. A rewind that fails sends nothing and falls back to the honest second
 * option, which the person then confirms by sending again.
 */
export function useInterfaceMessageActions({
  serverId,
  workerId,
  conversationScopeKey,
  connected,
  running,
  messageRewind,
  replyTurns,
  desktop,
  chrome,
  draft,
  setDraft,
  focusComposer,
  sendDraft,
  resendMessage,
}: Input) {
  const brain = Boolean(conversationScopeKey?.startsWith("brain-thread:"));
  const [edit, setEdit] = useState<MessageEdit | null>(null);
  const [rewinding, setRewinding] = useState(false);
  const [editError, setEditError] = useState<string>();
  const savedDraftRef = useRef("");
  const rewindingRef = useRef(false);
  const guard = useMemo(() => createResendGuard(), []);
  const latest = useRef({ draft, edit, sendDraft, setDraft });
  latest.current = { draft, edit, sendDraft, setDraft };

  const environment = useMemo<MessageActionEnvironment>(
    () => ({ messageRewind, brain, running, connected, canSelectText: !desktop }),
    [brain, connected, desktop, messageRewind, running],
  );

  // An edit belongs to one conversation; leaving it drops the edit.
  const scope = `${serverId}\u0000${workerId}\u0000${conversationScopeKey ?? ""}`;
  useEffect(() => {
    setEdit(null);
    setEditError(undefined);
    guard.clear();
    return () => {
      guard.clear();
    };
  }, [guard, scope]);

  const openEdit = useCallback(
    (subject: MessageActionSubject, state: Partial<MessageEdit> = {}, error?: string) => {
      if (!latest.current.edit) {
        savedDraftRef.current = latest.current.draft;
      }
      setEdit({
        id: subject.id,
        original: subject.body,
        mode: messageEditMode({ messageRewind, brain }),
        ...state,
      });
      setEditError(error);
      latest.current.setDraft(subject.body);
      focusComposer();
    },
    [brain, focusComposer, messageRewind],
  );

  const onEdit = useCallback(
    (subject: MessageActionSubject) => {
      if (rewindingRef.current) return;
      openEdit(subject);
    },
    [openEdit],
  );

  const cancelEdit = useCallback(() => {
    if (rewindingRef.current) return;
    setEdit(null);
    setEditError(undefined);
    latest.current.setDraft(savedDraftRef.current);
  }, []);

  const onRetry = useCallback(
    async (prompt: MessageActionSubject) => {
      if (rewindingRef.current || !guard.begin(prompt.id)) return;
      if (messageEditMode({ messageRewind, brain }) === "copy") {
        resendMessage(prompt.body);
        return;
      }
      rewindingRef.current = true;
      setRewinding(true);
      try {
        const { composerClean } = await wsClient.rewindMessage(serverId, workerId, prompt.id);
        if (composerClean) {
          resendMessage(prompt.body);
        } else {
          openEdit(prompt, { rewound: true }, INPUT_NOT_CLEAN);
        }
      } catch (error) {
        // Nothing was rewound or sent: the prompt waits in the composer as a copy.
        guard.release(prompt.id);
        openEdit(
          prompt,
          { mode: "copy" },
          error instanceof Error && error.message ? error.message : "Could not rewind.",
        );
      } finally {
        rewindingRef.current = false;
        setRewinding(false);
      }
    },
    [brain, guard, messageRewind, openEdit, resendMessage, serverId, workerId],
  );

  const sendEdited = useCallback(async () => {
    const current = latest.current.edit;
    if (!current) {
      latest.current.sendDraft();
      return;
    }
    if (rewindingRef.current || !editIsSendable(current, latest.current.draft)) return;
    if (current.mode === "rewind" && !current.rewound) {
      rewindingRef.current = true;
      setRewinding(true);
      setEditError(undefined);
      try {
        const { composerClean } = await wsClient.rewindMessage(serverId, workerId, current.id);
        if (!composerClean) {
          // The agent did step back, so this is not a failure to retry: the
          // edit is marked rewound and nothing is sent into a box that still
          // holds the old prompt.
          setEdit({ ...current, rewound: true });
          setEditError(INPUT_NOT_CLEAN);
          return;
        }
      } catch (error) {
        // Nothing was rewound or sent. The edit stays, now an honest copy.
        setEdit({ ...current, mode: "copy" });
        setEditError(error instanceof Error && error.message ? error.message : "Could not rewind.");
        return;
      } finally {
        rewindingRef.current = false;
        setRewinding(false);
      }
    }
    setEdit(null);
    setEditError(undefined);
    const restore = savedDraftRef.current;
    latest.current.sendDraft();
    // The composer empties on a send; give back what was there before the edit.
    if (restore) latest.current.setDraft(restore);
  }, [serverId, workerId]);

  const accessory = useMemo(
    () =>
      edit
        ? React.createElement(MessageEditChip, {
            original: edit.original,
            error: editError,
            busy: rewinding,
            chrome,
            onCancel: cancelEdit,
          })
        : null,
    [cancelEdit, chrome, edit, editError, rewinding],
  );

  const editBlocksSend = rewinding || (edit !== null && !editIsSendable(edit, draft));
  return { environment, replyTurns, onEdit, onRetry, sendEdited, accessory, editBlocksSend };
}
