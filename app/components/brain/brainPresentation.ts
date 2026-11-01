import { sessionFileMediaKindForPath } from "../../services/sessionFileMedia";
import type { BrainExecutorRef, BrainExecutorState } from "../../store/brain";

export function brainProviderLabel(value?: string): string {
  const normalized = value?.trim().toLowerCase();
  switch (normalized) {
    case "codex":
      return "Codex";
    case "cursor":
      return "Cursor Agent";
    case "grok":
      return "Grok";
    case "claude":
      return "Claude Code";
    case "tmux":
      return "tmux";
    default:
      return value?.trim() || "Custom";
  }
}

/**
 * The executors a person can choose: the ones installed on the computer,
 * signed-in ones first (otherwise in the computer's order). The rest are not
 * choices; `moreToInstall` says there are some, for one line pointing at
 * `mewla deps`.
 */
export function brainExecutorChoices(
  executors: readonly BrainExecutorRef[],
  states: Readonly<Record<string, BrainExecutorState>> | undefined,
): { choices: BrainExecutorRef[]; moreToInstall: boolean } {
  const installed = executors.filter((executor) => states?.[executor.id]?.installed);
  const signedIn = installed.filter((executor) => states?.[executor.id]?.signed_in);
  const others = installed.filter((executor) => !states?.[executor.id]?.signed_in);
  return { choices: [...signedIn, ...others], moreToInstall: installed.length < executors.length };
}

export function brainAdapterLabel(adapter?: BrainExecutorRef | null): string {
  if (!adapter) {
    return "";
  }
  if (adapter.name?.trim()) {
    return adapter.name.trim();
  }
  return brainProviderLabel(
    adapter.provider && adapter.provider !== "custom"
      ? adapter.provider
      : adapter.id,
  );
}

export function brainAdapterProviderKey(
  adapter?: BrainExecutorRef | null,
): string {
  const normalized = adapter?.provider?.trim().toLowerCase();
  if (
    normalized === "codex" ||
    normalized === "cursor" ||
    normalized === "grok" ||
    normalized === "claude" ||
    normalized === "tmux"
  ) {
    return normalized;
  }
  return "custom";
}

export function switchExecutorAccessibilityLabel(
  host?: BrainExecutorRef | null,
): string {
  return `Switch Brain host, ${brainAdapterLabel(host) || "unavailable"}`;
}

export function brainStatusLine({
  adapter,
}: {
  adapter?: BrainExecutorRef | null;
}): string {
  // Header stays compact — workspace paths live in the workspace viewer, not chrome.
  return brainAdapterLabel(adapter) || "Waiting for connection";
}

export function brainWorkspaceMarkdownPath(path: string): boolean {
  return /\.(md|markdown)$/i.test(path);
}

export function brainWorkspaceEntryAccessibilityLabel(
  kind: string,
  name: string,
): string {
  return `${kind === "directory" ? "Open folder" : "Open file"} ${name}`;
}

export function brainWorkspaceEntryIconName(
  kind: string,
  path: string,
): "folder" | "document-text" | "document" | "play" {
  if (kind === "directory") {
    return "folder";
  }
  if (sessionFileMediaKindForPath(path)) {
    return "play";
  }
  return brainWorkspaceMarkdownPath(path)
    ? "document-text"
    : "document";
}
