import { BrowserRequestError, type BrowserResource } from "./browser";
import type { StatusTone } from "../components/ui/StatusPill";

export type BrowserRecovery = "retry" | "reconnect" | "take_control" | "settings" | "restart" | "none";

/** A failure explained in product terms; the raw text only goes to diagnostics. */
export interface BrowserIssue {
  title: string;
  detail: string;
  tone: "warning" | "danger";
  recovery: BrowserRecovery;
  diagnostic: string;
}

export interface BrowserPresence {
  label: string;
  tone: StatusTone;
  detail: string;
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
    return issue("Browser isn’t on this server yet", `Update Zen on ${server} to keep a browser you can reuse.`, "retry", "warning");
  }
  if (status === 401 || status === 403) {
    return issue("This phone needs to pair again", `${server} didn’t accept this phone. Pair it again in Settings.`, "settings");
  }
  if (status === 503) {
    return issue("Browser is off on this server", `Browser isn’t running on ${server} right now. Try again in a moment.`, "retry", "warning");
  }
  if (/controlled by someone else/i.test(message)) {
    return issue("Someone else is in control", "Another device or an Agent task is using this browser. Try again after they release it.", "take_control", "warning");
  }
  if (/control changed|take control before/i.test(message)) {
    return issue("You’re no longer in control", "Take control again to keep browsing.", "take_control", "warning");
  }
  if (/could not be drained|needs restart/i.test(message)) {
    return issue("This browser needs a restart", "Close it, then open it again. Your sign-ins are kept.", "restart");
  }
  if (/browser not found|invalid browser resource/i.test(message)) {
    return issue("This browser was removed", `Refresh to see the browsers on ${server}.`, "retry", "warning");
  }
  if (/input is busy/i.test(message)) {
    return issue("Still sending your last action", "Wait a moment, then try again.", "none", "warning");
  }
  if (/timed out|disconnected|viewer closed|connection failed/i.test(message)) {
    return issue("Lost connection to the browser", `The browser is still open on ${server}. Reconnect to keep viewing it.`, "reconnect", "warning");
  }
  if (error instanceof TypeError || /network request failed|failed to fetch/i.test(message)) {
    return issue(`Can’t reach ${server}`, "Check that the server is online and this phone has a connection.", "retry");
  }
  return issue("That didn’t finish", "Try again. If it keeps happening, the details below can help.", "retry");
}

/** Where a browser stands. `controlling` is true only for this viewer's own control. */
export function browserPresence(resource: BrowserResource, controlling = false): BrowserPresence {
  if (resource.state === "needs_restart") {
    return { label: "Needs restart", tone: "warning", detail: "Close it and open it again. Sign-ins are kept." };
  }
  if (resource.state !== "running") {
    return { label: "Closed", tone: "neutral", detail: "Sign-ins are saved. Open it to continue where you left off." };
  }
  if (resource.control === "quiescing") {
    return { label: "Switching control", tone: "accent", detail: "Finishing the last action before control changes." };
  }
  if (resource.control === "human") {
    return controlling
      ? { label: "You’re in control", tone: "accent", detail: "Agent tasks wait until you release control." }
      : { label: "In use on another device", tone: "warning", detail: "Agent tasks wait until it’s released." };
  }
  if (resource.control === "agent") {
    return { label: "Agent is using it", tone: "accent", detail: "You can watch. Take control to pause the Agent." };
  }
  return { label: "Open", tone: "success", detail: "Ready for you or an Agent task." };
}

export function agentAvailability(resource: BrowserResource): { label: string; detail: string } {
  if (!resource.allow_agents) {
    return { label: "Agent tasks can’t use it", detail: "Turn on to let Codex or Claude sessions continue with these sign-ins." };
  }
  if (resource.state !== "running") {
    return { label: "Agent tasks can use it once it’s open", detail: "Open the browser before you start a session that needs it." };
  }
  return { label: "Agent tasks can use it", detail: "Choose it when you start a Codex or Claude session." };
}

export function recoveryLabel(recovery: BrowserRecovery): string | null {
  switch (recovery) {
    case "retry": return "Try again";
    case "reconnect": return "Reconnect";
    case "take_control": return "Take control";
    case "settings": return "Open Settings";
    case "restart": return "Close browser";
    default: return null;
  }
}
