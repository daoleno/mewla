/**
 * Editing one of the person's sent messages (a tap on it; copying is the system's selection, a long press).
 * Where the agent's CLI can step the conversation back to an earlier prompt, sending the edit rewinds to
 * that prompt first ("rewind"). Everywhere else, and always in Brain's own thread, it goes out as a new
 * message with the text prefilled ("copy").
 */
export type MessageEditMode = "rewind" | "copy";

export interface MessageActionSubject {
  role: "user" | "assistant";
  body: string;
  hasAttachments: boolean;
  /** Still being sent: not yet a message of the conversation. */
  pending: boolean;
}

export interface MessageActionEnvironment {
  /** A turn is running: rewinding or resending would collide with it. */
  running: boolean;
  connected: boolean;
}

export function messageEditMode(env: { messageRewind: boolean; brain: boolean }): MessageEditMode {
  return env.messageRewind && !env.brain ? "rewind" : "copy";
}

/** A sent text-only prompt of a connected, idle chat can be edited. */
export function isEditablePrompt(subject: MessageActionSubject, env: MessageActionEnvironment): boolean {
  return (
    subject.role === "user" &&
    !subject.pending &&
    !subject.hasAttachments &&
    subject.body.trim().length > 0 &&
    env.connected &&
    !env.running
  );
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

/** An edit goes out whenever it says something. */
export function editIsSendable(_edit: MessageEdit, draft: string): boolean {
  return draft.trim().length > 0;
}
