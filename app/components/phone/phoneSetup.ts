import type { PhoneStatus } from "../../modules/mewla-phone/src";

export type PhoneStepId = "developer" | "wifi" | "wireless" | "pair" | "helper";

export interface PhoneStep {
  id: PhoneStepId;
  title: string;
  /** One line only where the person cannot guess what to do. */
  hint?: string;
  done: boolean;
}

/** The checks in the order they can be done; the first undone one is what the screen offers. */
export function phoneSteps(status: PhoneStatus): PhoneStep[] {
  // A paired phone remembers Mewla: reinstalling the App keeps that, so only the helper is left. Wireless
  // debugging counts as done too when the App may turn it back on itself (the secure-settings grant).
  return [
    { id: "developer", title: "Developer options", hint: "Tap Build number 7 times", done: status.developer_options || status.paired },
    { id: "wifi", title: "Wi‑Fi", done: status.wifi },
    { id: "wireless", title: "Wireless debugging", done: status.wireless_debugging || (status.paired && status.self_healing) },
    { id: "pair", title: "Pairing", done: status.paired },
    { id: "helper", title: "Helper", done: status.helper },
  ];
}

export function nextPhoneStep(status: PhoneStatus): PhoneStep | null {
  return phoneSteps(status).find((step) => !step.done) ?? null;
}

export type PhoneSetupAction = "allow" | "setup" | "wifi" | "wireless" | "start";

export interface PhoneSetupPlan {
  /** What the one big button does now. */
  action: PhoneSetupAction;
  label: string;
  /** The row the person's eye should be on, and the short line under it. */
  current: PhoneStepId | null;
  note: string | null;
}

/**
 * The shortest way through setup: allow Mewla's accessibility service once, then it walks Settings
 * itself and the person only answers what Android reserves for them (the screen lock, the network
 * prompt). A step that stops getting anywhere falls back to its manual row. A note appears only
 * where the person has to do something; progress is the row's spinner.
 */
export function phoneSetupPlan(status: PhoneStatus, running: boolean): PhoneSetupPlan {
  const next = nextPhoneStep(status);
  const guide = status.guide;
  if (!next) return { action: "start", label: "Start", current: null, note: null };
  if (next.id === "helper") return { action: "start", label: "Start", current: "helper", note: status.problem ?? null };
  if (next.id === "wifi") return { action: "wifi", label: "Open Wi‑Fi", current: "wifi", note: null };
  // Already paired: never walk the pairing again. The one thing left is the switch the App may not flip itself.
  if (status.paired) return { action: "wireless", label: "Turn on Wireless debugging", current: "wireless", note: status.problem ?? null };
  if (!status.accessibility) {
    return {
      action: "allow",
      label: "Allow",
      current: next.id,
      note: status.restricted ? "App info › ⋮ › Allow restricted settings" : null,
    };
  }
  if (!running && !guide) return { action: "setup", label: "Set up", current: next.id, note: null };
  if (guide?.needs === "allow") return { action: "setup", label: "Set up", current: "wireless", note: "Tap Allow on the system prompt" };
  if (guide?.needs === "lock") return { action: "setup", label: "Set up", current: "developer", note: "Confirm your screen lock" };
  if (guide?.stalled) return { action: "setup", label: "Set up", current: next.id, note: next.hint ?? "Do this step yourself" };
  return { action: "setup", label: "Set up", current: next.id, note: null };
}

export type PhoneMode = "setup" | "idle" | "watching" | "driving" | "handoff" | "stopped";

/** What the screen shows: the setup list until everything is in place, then the agent's screen. */
export function phoneMode(status: PhoneStatus): PhoneMode {
  if (nextPhoneStep(status)) return "setup";
  if (status.state === "stopped") return "stopped";
  if (status.handoff) return "handoff";
  if (status.state === "person_in_control") return "driving";
  if (status.agent) return "watching";
  return "idle";
}

/** An agent is working on the phone and its screen is worth showing; stopped, idle and setup are not. */
export function phoneAgentWorking(status: PhoneStatus | null): boolean {
  if (!status?.enabled) return false;
  const mode = phoneMode(status);
  return mode === "watching" || mode === "driving" || mode === "handoff";
}


/**
 * What the float shows while the agent's display has nothing on it, or null once it shows a picture:
 * a black slab tells nobody anything. A spinner is waiting, with the app's name once it is known; a
 * failure says what failed.
 */
export function floatPlaceholder(
  status: Pick<PhoneStatus, "drawn" | "opening" | "launch_failed">,
): { text: string | null; failed: boolean } | null {
  if (status.drawn) return null;
  if (status.opening) return { text: status.opening, failed: false };
  if (status.launch_failed) return { text: `Couldn't open ${status.launch_failed}`, failed: true };
  return { text: null, failed: false };
}
