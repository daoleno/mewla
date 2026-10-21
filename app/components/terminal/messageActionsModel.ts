import type { ActionMenuItem } from "../ui/ActionMenu";

/**
 * What the message menu offers, and what each choice really does.
 *
 * Copy and Select Text work everywhere. Edit depends on the agent: where its
 * CLI can step the conversation back to an earlier prompt, sending the edit
 * rewinds to that prompt first ("rewind"). Everywhere else, and always in
 * Brain's own thread, it goes out as a new message with the text prefilled
 * ("copy"). Retry on the latest reply follows the same two modes.
 */
export type MessageEditMode = "rewind" | "copy";
export type MessageActionKind = "copy" | "select" | "edit";

export interface MessageActionSubject {
  id: string;
  role: "user" | "assistant";
  body: string;
  hasAttachments: boolean;
  /** Still being sent: not yet a message of the conversation. */
  pending: boolean;
  /** Assistant text that is still arriving. */
  streaming: boolean;
}

export interface MessageActionEnvironment {
  /** The Session's CLI can rewind (a daemon capability, never guessed here). */
  messageRewind: boolean;
  /** Brain's main thread is never rewound. */
  brain: boolean;
  /** A turn is running: rewinding or resending would collide with it. */
  running: boolean;
  connected: boolean;
  /** Select Text opens the message in a sheet (phone); desktop selects in place. */
  canSelectText: boolean;
}

/** The newest reply and the prompt it answers: what Retry runs again. */
export interface LatestReply {
  replyId: string;
  prompt: MessageActionSubject;
}

export function messageEditMode(env: Pick<MessageActionEnvironment, "messageRewind" | "brain">): MessageEditMode {
  return env.messageRewind && !env.brain ? "rewind" : "copy";
}

export function messageActionKinds(
  subject: MessageActionSubject,
  env: MessageActionEnvironment,
): MessageActionKind[] {
  if (subject.streaming || !subject.body.trim()) {
    return [];
  }
  const kinds: MessageActionKind[] = ["copy"];
  if (env.canSelectText) {
    kinds.push("select");
  }
  if (isRerunnablePrompt(subject, env)) {
    kinds.push("edit");
  }
  return kinds;
}

/** A sent text-only prompt of a connected, idle Session can be edited or retried. */
export function isRerunnablePrompt(subject: MessageActionSubject, env: MessageActionEnvironment): boolean {
  return (
    subject.role === "user" &&
    !subject.pending &&
    !subject.hasAttachments &&
    subject.body.trim().length > 0 &&
    env.connected &&
    !env.running
  );
}

const MENU_ITEMS: Record<MessageActionKind, Pick<ActionMenuItem, "label" | "icon">> = {
  copy: { label: "Copy", icon: "copy" },
  select: { label: "Select Text", icon: "text" },
  edit: { label: "Edit", icon: "edit" },
};

/** The sheet's rows for one message: Copy, Select Text, Edit, as each applies. */
export function messageMenuItems(
  kinds: readonly MessageActionKind[],
  run: (kind: MessageActionKind) => void,
): ActionMenuItem[] {
  return kinds.map((kind) => ({ key: kind, ...MENU_ITEMS[kind], onPress: () => run(kind) }));
}

/** The text copied: assistant Markdown and user text exactly as written. */
export function messageCopyText(subject: Pick<MessageActionSubject, "body">): string {
  return subject.body.replace(/\s+$/u, "");
}

export interface MessageEdit {
  /** The provider event the edit starts from. */
  id: string;
  /** The text as it was sent. */
  original: string;
  mode: MessageEditMode;
  /**
   * The agent already stepped back to this message but its input box was not
   * clean. The rewind is done and is never repeated; the text may go out once
   * the person has cleared the box.
   */
  rewound?: boolean;
}

/** An edit goes out whenever it says something; sending it unchanged is a retry. */
export function editIsSendable(_edit: MessageEdit, draft: string): boolean {
  return draft.trim().length > 0;
}

/**
 * Retry must not double-send on a double tap or a slow acknowledgement: a
 * prompt is held for the hold window after each tap.
 */
export const RESEND_HOLD_MS = 2500;

export function createResendGuard(now: () => number = Date.now) {
  const heldUntil = new Map<string, number>();
  return {
    begin(id: string): boolean {
      const current = now();
      for (const [key, until] of heldUntil) {
        if (until <= current) heldUntil.delete(key);
      }
      if (heldUntil.has(id)) return false;
      heldUntil.set(id, current + RESEND_HOLD_MS);
      return true;
    },
    release(id: string) {
      heldUntil.delete(id);
    },
    clear() {
      heldUntil.clear();
    },
  };
}
