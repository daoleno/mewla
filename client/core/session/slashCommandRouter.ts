/**
 * What sending a `/command` from the chat composer does, from the Expo app's
 * useCodexSlashCommandRouter.ts (a typed draft), useCodexSlashCommandPicker.ts
 * (a command picked from the list) and useCodexSlashCommandDialogs.ts (the
 * alerts). The hooks called setters; these return the action instead. Both
 * hooks first closed the composer's action menu, whatever the action.
 *
 * | Action | The UI then |
 * |---|---|
 * | `none` | Not a command: sends the draft as a message. |
 * | `insert` | An insert-only command: sends the draft as a message, as typed. |
 * | `prefill` | Sets the draft to `draft`, focuses the composer, and shows `alert` if set. |
 * | `alert` | Shows the alert (`slashCommandAlert`); the draft stays. |
 * | `status` | Clears the draft and opens the Codex status sheet. |
 * | `skills` | Clears the draft and opens the Codex skills sheet. |
 * | `new_chat` | `/new` or `/clear`: starts a new chat with `text` (startNewInterfaceChat). |
 * | `send` | Sends `text` to the Session as the slash command (sendSlashCommandToInterface). |
 */
import {
  requiresSlashCommandArgs,
  slashCommandAcceptsArgs,
  slashCommandHasArgs,
  slashCommandRequestFromDraft,
  type CodexSlashCommand,
} from "./codexSlashCommands";

export type SlashCommandAlertKind = "attachments" | "unavailable" | "unsupported";

export type SlashCommandAction =
  | { kind: "none" }
  | { kind: "insert"; command: CodexSlashCommand }
  | { kind: "prefill"; command: CodexSlashCommand; draft: string; alert?: "unavailable" }
  | { kind: "alert"; command: CodexSlashCommand; alert: "attachments" | "unsupported" }
  | { kind: "status"; command: CodexSlashCommand }
  | { kind: "skills"; command: CodexSlashCommand }
  | { kind: "new_chat"; command: CodexSlashCommand; text: string }
  | { kind: "send"; command: CodexSlashCommand; text: string };

/** The router's return value: false for `none` and `insert`, where the draft goes out as a message. */
export function slashCommandActionHandled(action: SlashCommandAction): boolean {
  return action.kind !== "none" && action.kind !== "insert";
}

/** A draft the person sent (routeDraftSubmission). */
export function routeSlashCommand(input: {
  draft: string;
  slashCommands: CodexSlashCommand[];
  /** The composer holds attachments. */
  hasAttachments: boolean;
}): SlashCommandAction {
  const request = slashCommandRequestFromDraft(input.draft, input.slashCommands);
  if (!request) {
    return { kind: "none" };
  }
  const { command, rawText, known } = request;
  if (input.hasAttachments && command.execution !== "native") {
    return { kind: "alert", command, alert: "attachments" };
  }
  if (
    requiresSlashCommandArgs(command) &&
    !slashCommandHasArgs(rawText, command)
  ) {
    return { kind: "prefill", command, draft: `${command.value} ` };
  }
  if (!known) {
    return { kind: "prefill", command, draft: `${command.value} `, alert: "unavailable" };
  }
  if (!command.chat_supported) {
    return { kind: "prefill", command, draft: rawText, alert: "unavailable" };
  }
  if (command.execution === "unsupported") {
    return { kind: "alert", command, alert: "unsupported" };
  }
  if (command.execution === "insert-only") {
    return { kind: "insert", command };
  }
  if (command.name === "status") {
    return { kind: "status", command };
  }
  if (command.execution === "native") {
    if (command.name === "new" || command.name === "clear") {
      return { kind: "new_chat", command, text: rawText };
    }
    if (command.name === "skills") {
      return { kind: "skills", command };
    }
    return { kind: "prefill", command, draft: `${command.value} ` };
  }
  if (command.name === "new" || command.name === "clear") {
    return { kind: "new_chat", command, text: rawText };
  }
  return { kind: "send", command, text: rawText };
}

/**
 * A command picked from the list (pickSlashCommand). Its `new_chat` and
 * `send` carry the command alone, sent with no attachments.
 */
export function routePickedSlashCommand(input: {
  command: CodexSlashCommand;
  hasAttachments: boolean;
}): SlashCommandAction {
  const { command } = input;
  if (input.hasAttachments) {
    return { kind: "prefill", command, draft: `${command.value} ` };
  }
  if (command.execution === "unsupported") {
    return { kind: "alert", command, alert: "unsupported" };
  }
  if (!command.chat_supported) {
    return { kind: "prefill", command, draft: `${command.value} `, alert: "unavailable" };
  }
  if (command.execution === "insert-only") {
    return { kind: "prefill", command, draft: `${command.value} ` };
  }
  if (command.name === "status") {
    return { kind: "status", command };
  }
  if (command.execution === "native") {
    if (command.name === "new" || command.name === "clear") {
      return { kind: "new_chat", command, text: command.value };
    }
    if (command.name === "skills") {
      return { kind: "skills", command };
    }
    return { kind: "prefill", command, draft: `${command.value} ` };
  }
  if (command.name === "new" || command.name === "clear") {
    return { kind: "new_chat", command, text: command.value };
  }
  if (slashCommandAcceptsArgs(command)) {
    return { kind: "prefill", command, draft: `${command.value} ` };
  }
  return { kind: "send", command, text: command.value };
}

export type SlashCommandAlertButton = {
  label: string;
  /** cancel closes the alert; open_terminal switches to the terminal; send_as_message sends the composed text and attachments as a message. */
  action: "cancel" | "open_terminal" | "send_as_message";
};

/** The alert's words and buttons, as useCodexSlashCommandDialogs showed them. */
export function slashCommandAlert(
  kind: SlashCommandAlertKind,
  command: CodexSlashCommand,
  canOpenTerminal: boolean,
): { title: string; message: string; buttons: SlashCommandAlertButton[] } {
  switch (kind) {
    case "unsupported":
      return {
        title: `${command.value} is not available`,
        message: "This command is hidden or internal in Codex.",
        buttons: [{ label: "OK", action: "cancel" }],
      };
    case "unavailable":
      return {
        title: `${command.value} is not available`,
        message: "This command opens an interactive Codex control that is not available in ChatUI yet.",
        buttons: canOpenTerminal
          ? [
              { label: "Cancel", action: "cancel" },
              { label: "Open Terminal", action: "open_terminal" },
            ]
          : [{ label: "OK", action: "cancel" }],
      };
    case "attachments":
      return {
        title: `${command.value} cannot use attachments here`,
        message: "Run the slash command without attachments, or send this as a normal message.",
        buttons: [
          { label: "Cancel", action: "cancel" },
          { label: "Send as Message", action: "send_as_message" },
        ],
      };
  }
}
