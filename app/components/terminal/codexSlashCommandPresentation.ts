import type { IconName } from "../icons/Icon";

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

export function slashCommandIcon(name: string): IconName {
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
