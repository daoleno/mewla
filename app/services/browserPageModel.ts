import { BrowserRequestError } from "./browser";

export type BrowserRecovery = "retry" | "reconnect" | "take_control" | "settings" | "none";

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
    return issue("This window needs a fresh start", "Stop the agent; its next browser action opens a new window. Sign-ins are kept.", "none");
  }
  if (/browser not found|invalid browser resource/i.test(message)) {
    return issue("This window was closed", "The agent's next browser action opens a new one.", "retry", "warning");
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

/** Bounded automatic reconnects after a dropped view; then one explicit action. */
export const RECONNECT_DELAYS_MS = [1000, 2000, 4000] as const;

export function recoveryLabel(recovery: BrowserRecovery): string | null {
  switch (recovery) {
    case "retry": return "Try again";
    case "reconnect": return "Reconnect";
    case "take_control": return "Take over";
    case "settings": return "Open Settings";
    default: return null;
  }
}
