/**
 * What the message menu offers, and what each choice really does.
 *
 * Copy and Resend work everywhere. Edit depends on the agent: where its CLI can
 * step the conversation back to an earlier prompt, editing rewinds to that
 * prompt and sends the edited text from there ("rewind"). Everywhere else, and
 * always in Brain's own thread, it prefills the composer with an edited copy
 * that goes out as a new message ("copy"). Nothing claims history changed when
 * it did not.
 */
export type MessageEditMode = "rewind" | "copy";
export type MessageActionKind = "copy" | "select" | "edit" | "resend";

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
  /** Native text selection is off in bubbles; the menu turns it on per message. */
  canSelectText: boolean;
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
  const editable =
    subject.role === "user" && !subject.pending && !subject.hasAttachments && env.connected;
  if (editable && !env.running) {
    kinds.push("edit", "resend");
  }
  return kinds;
}

/** The text copied: assistant Markdown and user text exactly as written. */
export function messageCopyText(subject: Pick<MessageActionSubject, "body">): string {
  return subject.body.replace(/\s+$/u, "");
}

export interface MessageEdit {
  /** The provider event the edit starts from. */
  id: string;
  /** The text as it was sent, to tell an unchanged edit from a changed one. */
  original: string;
  mode: MessageEditMode;
  /**
   * The agent already stepped back to this message but its input box was not
   * clean. The rewind is done and is never repeated; the same text may go out
   * unchanged once the person has cleared the box.
   */
  rewound?: boolean;
}

/** An edited message goes out only when it says something different. */
export function editIsSendable(edit: MessageEdit, draft: string): boolean {
  const next = draft.trim();
  return next.length > 0 && (edit.rewound === true || next !== edit.original.trim());
}

/**
 * Resend must not double-send on a double tap or a slow acknowledgement: a
 * message is held for the hold window after each tap, and for as long as its
 * previous resend is still unsettled.
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
