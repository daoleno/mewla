/**
 * Codex slash commands in the chat composer, from the Expo app's
 * components/terminal/CodexSlashCommands.ts (without its fetching hook),
 * codexSlashCommandPresentation.ts, and the wire types from services/websocket.ts.
 */

// services/websocket.ts
export interface CodexSlashCommand {
  value: string;
  name: string;
  title: string;
  description: string;
  source?: string;
  category: CodexSlashCommandCategory;
  execution: CodexSlashCommandExecution;
  input: CodexSlashCommandInput;
  output: CodexSlashCommandOutput;
  interactive: boolean;
  chat_supported: boolean;
  terminal_supported: boolean;
}

export type CodexSlashCommandCategory =
  | "session"
  | "navigation"
  | "settings"
  | "tools"
  | "management"
  | "debug"
  | "danger"
  | "unknown"
  | string;

export type CodexSlashCommandExecution =
  "terminal-required" | "insert-only" | "native" | "unsupported" | string;

export interface CodexSlashCommandInput {
  kind: "none" | "inline-args" | "form" | "picker" | "freeform" | string;
  placeholder?: string;
  picker?: string;
  required?: boolean;
}

export interface CodexSlashCommandOutput {
  kind:
    | "none"
    | "markdown"
    | "monospace-log"
    | "diff"
    | "status-card"
    | "management-screen"
    | "terminal"
    | string;
}

export interface CodexSlashCommandSnapshot {
  source?: string;
  commands: CodexSlashCommand[];
}

// components/terminal/codexSlashCommandPresentation.ts

export function slashCommandTitle(name: string) {
  return name
    .split("-")
    .filter(Boolean)
    .map((part) => {
      const lower = part.toLowerCase();
      if (lower === "ide" || lower === "mcp") {
        return lower.toUpperCase();
      }
      return `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`;
    })
    .join(" ");
}

/** Empty-list copy: an unloaded daemon list is not the same as no match. */
export function slashCommandEmptyText(listUnavailable: boolean) {
  return listUnavailable ? "Codex commands unavailable" : "No matching command";
}

/** An icon name; the UI owns the icon set. */
export function slashCommandIcon(name: string): string {
  switch (name) {
    case "model":
      return "chip";
    case "fast":
      return "flash";
    case "ide":
      return "code";
    case "permissions":
    case "approve":
    case "test-approval":
      return "shield-check";
    case "keymap":
      return "keypad";
    case "setup-default-sandbox":
    case "sandbox-add-read-dir":
      return "lock-open";
    case "vim":
      return "edit";
    case "experimental":
      return "flask";
    case "memories":
      return "library";
    case "skills":
      return "construct";
    case "hooks":
      return "link";
    case "review":
      return "search";
    case "rename":
    case "title":
      return "text";
    case "new":
      return "add-circle";
    case "resume":
      return "play-forward";
    case "fork":
    case "side":
      return "git-branch";
    case "init":
      return "document-text";
    case "goal":
      return "flag";
    case "copy":
      return "copy";
    case "raw":
      return "reorder";
    case "diff":
      return "git-compare";
    case "mention":
      return "at";
    case "status":
      return "pulse";
    case "debug-config":
    case "debug-m-drop":
    case "debug-m-update":
      return "bug";
    case "statusline":
      return "reader";
    case "pets":
      return "happy";
    case "mcp":
      return "server";
    case "apps":
    case "plugins":
      return "puzzle";
    case "logout":
    case "quit":
    case "exit":
      return "exit";
    case "feedback":
      return "chatbox-dots";
    case "rollout":
      return "map";
    case "ps":
      return "layers";
    case "stop":
      return "stop-circle";
    case "clear":
      return "trash";
    case "personality":
      return "person-circle";
    case "realtime":
    case "settings":
      return "mic";
    case "agent":
    case "subagents":
      return "people";
    case "btw":
      return "chat";
    default:
      return "terminal";
  }
}

// components/terminal/CodexSlashCommands.ts

export type SlashCommandRequest = {
  command: CodexSlashCommand;
  rawText: string;
  known: boolean;
};

type LocalSlashCommandCapability = Pick<
  CodexSlashCommand,
  | "category"
  | "execution"
  | "input"
  | "output"
  | "interactive"
  | "chat_supported"
  | "terminal_supported"
>;

type SlashCommandOverride = Partial<
  Omit<CodexSlashCommand, "name" | "value" | "source">
>;

const CHATUI_SLASH_COMMAND_OVERRIDES: Record<string, SlashCommandOverride> = {
  new: {
    execution: "native",
    input: inputNone(),
    output: { kind: "none" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  clear: {
    execution: "native",
    input: inputNone(),
    output: { kind: "none" },
    interactive: false,
    chat_supported: false,
    terminal_supported: true,
  },
  skills: {
    execution: "native",
    input: inputNone(),
    output: { kind: "management-screen" },
    interactive: true,
    chat_supported: true,
    terminal_supported: true,
  },
  status: {
    execution: "native",
    input: inputNone(),
    output: { kind: "management-screen" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  init: {
    execution: "terminal-required",
    input: inputNone(),
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: false,
    terminal_supported: true,
  },
  rename: {
    execution: "terminal-required",
    input: requiredFreeformInput("new thread title"),
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  review: {
    execution: "terminal-required",
    input: requiredFreeformInput("review instructions"),
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  goal: {
    execution: "terminal-required",
    input: optionalFreeformInput("optional goal text"),
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  stop: {
    chat_supported: false,
  },
};

const HIDDEN_CHATUI_SLASH_COMMAND_NAMES = new Set([
  "compact",
  "copy",
  "diff",
  "mcp",
  "plan",
  "ps",
  "rollout",
  "theme",
]);

export function slashCommandRequestFromDraft(
  draft: string,
  commands: CodexSlashCommand[],
): SlashCommandRequest | null {
  const trimmedStart = draft.trimStart();
  if (!trimmedStart.startsWith("/")) {
    return null;
  }
  const firstLine = trimmedStart.split(/\r?\n/, 1)[0] || "";
  const match = /^\/([a-z][a-z0-9-]*)(?:\s|$)/.exec(firstLine);
  if (!match) {
    return null;
  }
  const name = match[1];
  const command = commands.find((candidate) => candidate.name === name);
  if (command) {
    return { command, rawText: trimmedStart, known: true };
  }
  return {
    command: {
      value: `/${name}`,
      name,
      title: slashCommandTitle(name),
      description: "Unknown Codex slash command",
      source: "draft",
      ...fallbackSlashCommandCapability(name),
    },
    rawText: trimmedStart,
    known: false,
  };
}

export function slashCommandAcceptsArgs(command: CodexSlashCommand) {
  return (
    command.input.kind === "inline-args" ||
    command.input.kind === "freeform" ||
    command.input.kind === "form"
  );
}

export function requiresSlashCommandArgs(command: CodexSlashCommand) {
  if (!slashCommandAcceptsArgs(command)) {
    return false;
  }
  if (typeof command.input.required === "boolean") {
    return command.input.required;
  }
  return false;
}

export function slashCommandHasArgs(rawText: string, command: CodexSlashCommand) {
  const args = rawText.trimStart().slice(command.value.length).trim();
  return args.length > 0;
}

export function filterSlashCommands(
  commands: CodexSlashCommand[],
  commandQuery: string,
) {
  if (!commandQuery.startsWith("/")) {
    return [];
  }
  const query = commandQuery.slice(1).toLowerCase();
  const visibleCommands = commands.filter(isVisibleChatSlashCommand);
  if (!query) {
    return visibleCommands;
  }

  return visibleCommands
    .map((command, index) => {
      const name = command.name.toLowerCase();
      const value = command.value.toLowerCase();
      const title = command.title.toLowerCase();
      const description = command.description.toLowerCase();
      let score = Number.POSITIVE_INFINITY;
      if (name === query || value === `/${query}`) {
        score = 0;
      } else if (name.startsWith(query) || value.startsWith(`/${query}`)) {
        score = 1;
      } else if (title.startsWith(query)) {
        score = 2;
      } else if (name.includes(query) || value.includes(query)) {
        score = 3;
      } else if (title.includes(query)) {
        score = 4;
      } else if (description.includes(query)) {
        score = 5;
      }
      return { command, index, score };
    })
    .filter((entry) => Number.isFinite(entry.score))
    .sort((a, b) => a.score - b.score || a.index - b.index)
    .map((entry) => entry.command);
}

/**
 * The command list the composer offers, from the daemon's `codex_slash_commands`
 * reply: names normalized, ChatUI's overrides applied, duplicates and hidden
 * commands dropped. (The Expo app's `useCodexSlashCommands` hook fetched the
 * list while the screen was focused and connected, only for Codex, kept a
 * loaded list across focus and reconnect, and cleared it when the server or
 * agent kind changed; that glue is the UI's now.)
 */
export function buildChatuiSlashCommands(daemonCommands: CodexSlashCommand[]) {
  const commands: CodexSlashCommand[] = [];
  const seen = new Set<string>();
  for (const command of daemonCommands) {
    const normalized = normalizeSlashCommand(command);
    if (!normalized || seen.has(normalized.name) || isHiddenSlashCommand(normalized.name)) {
      continue;
    }
    seen.add(normalized.name);
    commands.push(normalized);
  }
  return commands;
}

function normalizeSlashCommand(command: CodexSlashCommand) {
  const name = command.name.trim().replace(/^\//, "");
  if (!name) {
    return null;
  }
  const override = CHATUI_SLASH_COMMAND_OVERRIDES[name] ?? {};
  return {
    value: `/${name}`,
    name,
    title: override.title?.trim() || command.title?.trim() || slashCommandTitle(name),
    description: override.description?.trim() || command.description?.trim() || "Codex slash command",
    source: command.source || "codex",
    category: override.category || command.category || "unknown",
    execution: override.execution || command.execution || "terminal-required",
    input: mergeSlashCommandInput(command.input, override.input),
    output: mergeSlashCommandOutput(command.output, override.output),
    interactive: override.interactive ?? command.interactive,
    chat_supported: override.chat_supported ?? command.chat_supported,
    terminal_supported:
      override.terminal_supported ?? command.terminal_supported ?? command.execution !== "unsupported",
  } satisfies CodexSlashCommand;
}

function mergeSlashCommandInput(
  baseInput: CodexSlashCommand["input"],
  overrideInput?: CodexSlashCommand["input"],
) {
  return {
    kind: overrideInput?.kind || baseInput?.kind || "none",
    placeholder: overrideInput?.placeholder ?? baseInput?.placeholder,
    picker: overrideInput?.picker ?? baseInput?.picker,
    required: overrideInput?.required ?? baseInput?.required,
  };
}

function mergeSlashCommandOutput(
  baseOutput: CodexSlashCommand["output"],
  overrideOutput?: CodexSlashCommand["output"],
) {
  return {
    kind: overrideOutput?.kind || baseOutput?.kind || "terminal",
  };
}

function inputNone(): CodexSlashCommand["input"] {
  return { kind: "none" };
}

function optionalFreeformInput(placeholder: string): CodexSlashCommand["input"] {
  return { kind: "freeform", placeholder, required: false };
}

function requiredFreeformInput(placeholder: string): CodexSlashCommand["input"] {
  return { kind: "freeform", placeholder, required: true };
}

function isVisibleChatSlashCommand(command: CodexSlashCommand) {
  return (
    command.chat_supported &&
    command.execution !== "unsupported" &&
    !isHiddenSlashCommand(command.name)
  );
}

function isHiddenSlashCommand(name: string) {
  const normalized = name.toLowerCase();
  return HIDDEN_CHATUI_SLASH_COMMAND_NAMES.has(normalized) || normalized.startsWith("debug-");
}

function fallbackSlashCommandCapability(name: string): LocalSlashCommandCapability {
  return {
    category: name.startsWith("debug-") ? "debug" : "unknown",
    execution: "unsupported",
    input: { kind: "none" },
    output: { kind: "none" },
    interactive: true,
    chat_supported: false,
    terminal_supported: false,
  };
}
