/** From the Expo app's services/agentCommands.ts, plus the New session presets from components/terminal/NewTerminalLaunchPresetList.tsx. */
import type { AgentKind } from "./workerPresentation";

const QUOTE_TRIM_RE = /^['"]|['"]$/g;

export const CLAUDE_CODE_COMMAND = "claude --permission-mode bypassPermissions";
export const CODEX_COMMAND = "codex --dangerously-bypass-approvals-and-sandbox";
export const CURSOR_AGENT_COMMAND = "cursor-agent --force --sandbox disabled";
export const GROK_COMMAND =
  "grok --no-alt-screen --permission-mode bypassPermissions";
export const ANTIGRAVITY_COMMAND = "agy --dangerously-skip-permissions";
export const DSH_COMMAND = "dsh";
export const PI_COMMAND = "pi";
export const OPENCODE_AUTO_COMMAND = "opencode --auto";

/** DSH is a native session/terminal launcher; its browser UI is a service. */
export const DSH_CAPABILITIES = {
  chat: false,
  mode: "terminal",
  nativeModelSelection: true,
  webServiceUnit: "dsh-web.service",
} as const;

function normalizeCommand(command?: string) {
  return command?.trim().toLowerCase() || "";
}

export function commandBinary(command?: string) {
  const normalized = normalizeCommand(command);
  if (!normalized) {
    return "";
  }

  const token = normalized.split(/\s+/, 1)[0]?.replace(QUOTE_TRIM_RE, "") || "";
  const parts = token.split("/");
  return parts[parts.length - 1] || token;
}

// components/terminal/NewTerminalLaunchPresetList.tsx
export type NewTerminalLaunchPreset = {
  key: string;
  kind: AgentKind;
  label: string;
  command: string;
};

/** The New session sheet's agent choices, in order; Shell launches the login shell. */
export const LAUNCH_PRESETS: readonly NewTerminalLaunchPreset[] = [
  { key: "shell", kind: "terminal", label: "Shell", command: "" },
  {
    key: "claude",
    kind: "claude",
    label: "Claude",
    command: CLAUDE_CODE_COMMAND,
  },
  { key: "codex", kind: "codex", label: "Codex", command: CODEX_COMMAND },
  { key: "cursor", kind: "cursor", label: "Cursor", command: CURSOR_AGENT_COMMAND },
  { key: "grok", kind: "grok", label: "Grok", command: GROK_COMMAND },
  { key: "pi", kind: "pi", label: "Pi", command: PI_COMMAND },
  { key: "opencode", kind: "opencode", label: "OpenCode", command: OPENCODE_AUTO_COMMAND },
  { key: "antigravity", kind: "antigravity", label: "Antigravity", command: ANTIGRAVITY_COMMAND },
  { key: "dsh", kind: "dsh", label: "DSH", command: DSH_COMMAND },
];
