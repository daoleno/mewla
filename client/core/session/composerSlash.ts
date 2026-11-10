/**
 * The chat composer's slash commands, as the Session screen uses them: whether
 * the command menu shows (from the Expo app's InterfaceChatSurfaceModel.ts), and
 * what a send or a picked command does (on slashCommandRouter.ts, with the
 * alerts' words from useCodexSlashCommandDialogs). Slash commands are Codex's
 * only; for any other agent a send is the text as typed.
 */
import type { AgentKind } from "../sessions/workerPresentation";
import { chatAgentSupportsSlashCommands } from "./chatComposerPresentation";
import { filterSlashCommands, type CodexSlashCommand } from "./codexSlashCommands";
import {
  routePickedSlashCommand,
  routeSlashCommand,
  slashCommandAlert,
  type SlashCommandAction,
  type SlashCommandAlertButton,
} from "./slashCommandRouter";

export type ComposerAlert = { title: string; message: string; buttons: SlashCommandAlertButton[] };

/**
 * What the Session does with a send or a picked command. Each one closes the "+" menu.
 *
 * | kind | The Session then |
 * |---|---|
 * | `send` | Sends `text` (`send_input`). With `command` set it is the slash command itself: from a pick, sent without attachments. |
 * | `prefill` | Sets the draft to `text`, focuses the composer, then shows `alert` if set. |
 * | `alert` | Shows the alert; the draft stays. Its buttons: `cancel`, `open_terminal` (switch to the terminal), `send_as_message` (send the draft and attachments as a message). |
 * | `status` | Clears the draft and opens the Codex status sheet. |
 * | `skills` | Clears the draft and opens the Codex skills sheet. |
 */
export type ComposerSendRoute =
  | { kind: "send"; text: string; command?: CodexSlashCommand }
  | { kind: "prefill"; text: string; alert?: ComposerAlert }
  | ({ kind: "alert" } & ComposerAlert)
  | { kind: "status" }
  | { kind: "skills" };

export type ComposerSlashContext = {
  agentKind: AgentKind;
  /** The list from `chatSlashCommands` (ChatUI's overrides applied). */
  slashCommands: CodexSlashCommand[];
  /** The composer holds attachments. */
  hasAttachments?: boolean;
  /** The Session has a terminal to switch to ("Open Terminal" in the unavailable alert). */
  canOpenTerminal?: boolean;
};

function fromAction(text: string, action: SlashCommandAction, canOpenTerminal: boolean): ComposerSendRoute {
  switch (action.kind) {
    case "none":
    case "insert":
      return { kind: "send", text };
    case "prefill":
      return action.alert
        ? { kind: "prefill", text: action.draft, alert: slashCommandAlert(action.alert, action.command, canOpenTerminal) }
        : { kind: "prefill", text: action.draft };
    case "alert":
      return { kind: "alert", ...slashCommandAlert(action.alert, action.command, canOpenTerminal) };
    case "status":
      return { kind: "status" };
    case "skills":
      return { kind: "skills" };
    // The Expo app sent `/new` and `/clear` to the agent as input, like any other command.
    case "new_chat":
    case "send":
      return { kind: "send", text: action.text, command: action.command };
  }
}

/** What sending the composer's `text` does. */
export function routeComposerSend(text: string, ctx: ComposerSlashContext): ComposerSendRoute {
  if (!chatAgentSupportsSlashCommands(ctx.agentKind)) return { kind: "send", text };
  const action = routeSlashCommand({ draft: text, slashCommands: ctx.slashCommands, hasAttachments: Boolean(ctx.hasAttachments) });
  return fromAction(text, action, Boolean(ctx.canOpenTerminal));
}

/** What picking `command` from the menu does. */
export function routeComposerPick(command: CodexSlashCommand, ctx: Omit<ComposerSlashContext, "slashCommands" | "agentKind">): ComposerSendRoute {
  const action = routePickedSlashCommand({ command, hasAttachments: Boolean(ctx.hasAttachments) });
  return fromAction(command.value, action, Boolean(ctx.canOpenTerminal));
}

export type SlashMenuState = {
  /** The draft without its leading space: the query the list is filtered by. */
  commandQuery: string;
  /** The draft is a `/word` being typed (no space or newline yet), for a Codex Session. */
  slashQueryActive: boolean;
  /** The commands the menu lists: those matching the query, or all while it is not active. */
  visibleCommands: CodexSlashCommand[];
  /** The "+" menu's composer actions (Attach File) show. */
  showActions: boolean;
  /** The command list (or its empty line) shows. */
  showCommandList: boolean;
  /** Anything of the menu shows. */
  open: boolean;
};

/**
 * Whether the menu shows and what it lists. `pinned` is the "+" button's
 * state; typing `/x` opens the command list without it, and a normal draft
 * hides the command list even while pinned.
 */
export function slashMenuState(input: {
  text: string;
  agentKind: AgentKind;
  commands: CodexSlashCommand[];
  connected: boolean;
  pinned: boolean;
}): SlashMenuState {
  const commandQuery = input.text.trimStart();
  const enabled = chatAgentSupportsSlashCommands(input.agentKind);
  const slashQueryActive = enabled && commandQuery.startsWith("/") && !commandQuery.includes(" ") && !commandQuery.includes("\n");
  const normalDraftActive = commandQuery.length > 0 && !slashQueryActive;
  const visibleCommands = enabled ? filterSlashCommands(input.commands, slashQueryActive ? commandQuery : "/") : [];
  const showActions = input.connected && input.pinned;
  const showCommandList =
    enabled &&
    input.connected &&
    (input.pinned || slashQueryActive) &&
    (slashQueryActive || !normalDraftActive) &&
    (slashQueryActive || visibleCommands.length > 0);
  return { commandQuery, slashQueryActive, visibleCommands, showActions, showCommandList, open: showActions || showCommandList };
}

const COMMAND_GROUPS = [
  { key: "session", label: "Session" },
  { key: "navigation", label: "Navigation" },
  { key: "tools", label: "Project Tools" },
  { key: "settings", label: "Settings" },
  { key: "management", label: "Management" },
] as const;

export type SlashCommandGroup = { key: string; label: string; commands: CodexSlashCommand[] };

/** The menu's sections, in the Expo order; any other category falls under Other. Empty ones are left out. */
export function groupSlashCommands(commands: CodexSlashCommand[]): SlashCommandGroup[] {
  const known = new Set<string>(COMMAND_GROUPS.map((group) => group.key));
  const groups: SlashCommandGroup[] = COMMAND_GROUPS.map((group) => ({
    key: group.key,
    label: group.label,
    commands: commands.filter((command) => command.category === group.key),
  }));
  groups.push({ key: "other", label: "Other", commands: commands.filter((command) => !known.has(command.category)) });
  return groups.filter((group) => group.commands.length > 0);
}

/**
 * The draft after picking a skill (Expo's insertSkillMention): `$name ` is
 * added to a message being written, and replaces an empty draft or a `/command`.
 */
export function insertSkillMention(draft: string, name: string): string {
  const mention = `$${name}`;
  if (draft.trim() && !draft.trimStart().startsWith("/")) {
    return `${draft}${draft.endsWith(" ") || draft.endsWith("\n") ? "" : " "}${mention} `;
  }
  return `${mention} `;
}
