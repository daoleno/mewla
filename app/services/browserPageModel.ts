import { BrowserRequestError, type BrowserResource } from "./browser";

export type BrowserRecovery = "retry" | "reconnect" | "take_control" | "settings" | "restart" | "none";

/** A failure explained in product terms; the raw text only goes to diagnostics. */
export interface BrowserIssue {
  title: string;
  detail: string;
  tone: "warning" | "danger";
  recovery: BrowserRecovery;
  diagnostic: string;
}

export function browserIssue(error: unknown, server: string): BrowserIssue | null {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  if ((error instanceof Error && error.name === "AbortError") || /cancelled/i.test(message)) return null;
  const status = error instanceof BrowserRequestError ? error.status : 0;
  const raw = error instanceof BrowserRequestError ? error.detail || message : message;
  const diagnostic = [status ? `HTTP ${status}` : "", raw].filter(Boolean).join(" · ") || "No details";
  const issue = (title: string, detail: string, recovery: BrowserRecovery, tone: BrowserIssue["tone"] = "danger"): BrowserIssue =>
    ({ title, detail, recovery, tone, diagnostic });
  if (status === 404 || status === 405) {
    return issue("Browser isn't on this server yet", `Update Mewla on ${server} to use Browser.`, "retry", "warning");
  }
  if (status === 401 || status === 403) {
    return issue("This phone needs to pair again", `${server} didn't accept this phone. Pair again in Settings.`, "settings");
  }
  if (status === 503) {
    return issue("Browser is off on this server", "Try again in a moment.", "retry", "warning");
  }
  if (/controlled by someone else/i.test(message)) {
    return issue("Someone else is in control", "Another device or an Agent is using it.", "take_control", "warning");
  }
  if (/control changed|take control before/i.test(message)) {
    return issue("You're no longer in control", "Another device or an Agent took over.", "take_control", "warning");
  }
  if (/could not be drained|needs restart/i.test(message)) {
    return issue("This browser needs a restart", "Close it, then open it again. Your sign-ins are kept.", "restart");
  }
  if (/browser not found|invalid browser resource/i.test(message)) {
    return issue("This browser was removed", "Try again to open another browser.", "retry", "warning");
  }
  if (/input is busy/i.test(message)) {
    return issue("Still sending your last action", "Wait a moment, then try again.", "none", "warning");
  }
  if (/timed out|disconnected|viewer closed|connection failed/i.test(message)) {
    return issue("Lost connection to the browser", `Still open on ${server}.`, "reconnect", "warning");
  }
  if (error instanceof TypeError || /network request failed|failed to fetch/i.test(message)) {
    // Offline is a warning, never a failure: nothing broke on the server.
    return issue(`Can't reach ${server}`, "Check the server and this phone's connection.", "retry", "warning");
  }
  return issue("That didn't finish", "If it keeps failing, check Connection details in the options menu.", "retry");
}

export const DEFAULT_BROWSER_NAME = "Browser";

/** Bounded automatic reconnects after a dropped view; then one explicit action. */
export const RECONNECT_DELAYS_MS = [1000, 2000, 4000] as const;

/** Remembered browser first, then one that is already running, then the oldest. */
export function pickDefaultBrowser(resources: readonly BrowserResource[], rememberedId: string | null): BrowserResource | null {
  return resources.find((r) => r.id === rememberedId)
    ?? resources.find((r) => r.state === "running")
    ?? resources[0]
    ?? null;
}

/**
 * What opening the viewer may do about input. Only an idle browser is claimed
 * automatically; a current controller (an Agent or another device) is never
 * overridden without an explicit takeover.
 */
export function controlOffer(resource: BrowserResource): "acquire" | "takeover" | "wait" {
  if (resource.state !== "running" || resource.control === "quiescing") return "wait";
  if (resource.control === "idle") return "acquire";
  return "takeover";
}

/** Session launches can attach Browser only to Codex and Claude commands. */
export function supportsBrowserAttachment(command: string): boolean {
  const argv = command.trim().split(/\s+/).filter((part) => !/^[A-Za-z_][A-Za-z0-9_]*=/.test(part));
  const executable = (argv[0] ?? "").split("/").pop() ?? "";
  return executable === "codex" || executable === "claude";
}

/** A session-sheet Browser selection; `chosen` means the user picked (even None). */
export interface BrowserChoice { id?: string; automatic: boolean; chosen: boolean }

/** A preselected default only rides along with commands that can attach it. */
export function launchBrowserId(choice: BrowserChoice, command: string): string | undefined {
  return choice.automatic && !supportsBrowserAttachment(command) ? undefined : choice.id;
}

/** Automatic preselection never overrides the user's own choice. */
export function chooseBrowser(current: BrowserChoice, id: string | undefined, automatic: boolean): BrowserChoice {
  if (automatic && current.chosen) return current;
  return { id, automatic, chosen: current.chosen || !automatic };
}

export function recoveryLabel(recovery: BrowserRecovery): string | null {
  switch (recovery) {
    case "retry": return "Try again";
    case "reconnect": return "Reconnect";
    case "take_control": return "Take over";
    case "settings": return "Open Settings";
    case "restart": return "Close browser";
    default: return null;
  }
}
