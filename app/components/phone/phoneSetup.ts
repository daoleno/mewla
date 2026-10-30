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
  return [
    { id: "developer", title: "Developer options", hint: "Tap Build number 7 times", done: status.developer_options },
    { id: "wifi", title: "Wi‑Fi", done: status.wifi },
    { id: "wireless", title: "Wireless debugging", done: status.wireless_debugging },
    { id: "pair", title: "Pairing", done: status.paired },
    { id: "helper", title: "Helper", done: status.helper },
  ];
}

export function nextPhoneStep(status: PhoneStatus): PhoneStep | null {
  return phoneSteps(status).find((step) => !step.done) ?? null;
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
