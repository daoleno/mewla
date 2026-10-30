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

export type PhoneSetupAction = "allow" | "setup" | "wifi" | "start";

export interface PhoneSetupPlan {
  /** What the one big button does now. */
  action: PhoneSetupAction;
  label: string;
  /** The row the person's eye should be on, and the short line under it. */
  current: PhoneStepId | null;
  note: string | null;
}

const GUIDE_NOTES: Record<"developer" | "wireless" | "pair", string> = {
  developer: "Tapping Build number",
  wireless: "Turning on Wireless debugging",
  pair: "Pairing",
};

/**
 * The shortest way through setup: allow Mewla's accessibility service once, then it walks Settings
 * itself and the person only answers what Android reserves for them (the screen lock, the network
 * prompt). A step that stops getting anywhere falls back to its manual row.
 */
export function phoneSetupPlan(status: PhoneStatus, running: boolean): PhoneSetupPlan {
  const next = nextPhoneStep(status);
  const guide = status.guide;
  if (!next) return { action: "start", label: "Start", current: null, note: null };
  if (next.id === "helper") return { action: "start", label: "Start", current: "helper", note: null };
  if (next.id === "wifi") return { action: "wifi", label: "Open Wi‑Fi", current: "wifi", note: null };
  if (!status.accessibility) {
    return {
      action: "allow",
      label: "Allow",
      current: next.id,
      note: status.restricted ? "Android blocks it for apps installed from a file: App info › ⋮ › Allow restricted settings" : null,
    };
  }
  if (!running && !guide) return { action: "setup", label: "Set up", current: next.id, note: null };
  if (guide?.needs === "allow") return { action: "setup", label: "Set up", current: "wireless", note: "Tap Allow on the system prompt" };
  if (guide?.needs === "lock") return { action: "setup", label: "Set up", current: "developer", note: "Confirm your screen lock" };
  if (guide?.stalled) return { action: "setup", label: "Set up", current: next.id, note: next.hint ?? "Do this step yourself" };
  const step = guide?.step ?? (next.id === "developer" || next.id === "wireless" ? next.id : "pair");
  return { action: "setup", label: "Set up", current: next.id, note: GUIDE_NOTES[step] };
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

/** Whether the agent's screen floats over the page the person is on: only while an agent works, and never on the Phone page itself. */
export function phoneFloatVisible(status: PhoneStatus | null, pathname: string): boolean {
  if (!status || !phoneAgentWorking(status) || status.w <= 0 || status.h <= 0) return false;
  return pathname !== "/phone" && !pathname.startsWith("/phone/");
}

export type FloatCorner = "top-left" | "top-right" | "bottom-left" | "bottom-right";

export const DEFAULT_FLOAT_CORNER: FloatCorner = "top-right";

export function parseFloatCorner(value: string | null): FloatCorner {
  return value === "top-left" || value === "top-right" || value === "bottom-left" || value === "bottom-right"
    ? value
    : DEFAULT_FLOAT_CORNER;
}

/** The free rectangle the float may sit in: above the composer and the keyboard, under the header. */
export interface FloatArea {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** Where the float's top-left corner goes for a corner of the free area. */
export function floatOrigin(corner: FloatCorner, area: FloatArea, size: { width: number; height: number }, margin: number) {
  const x = corner.endsWith("left") ? area.left + margin : area.right - margin - size.width;
  const y = corner.startsWith("top") ? area.top + margin : area.bottom - margin - size.height;
  // A tiny area keeps the float on screen rather than off its top or left.
  return { x: Math.max(area.left, x), y: Math.max(area.top, y) };
}

/** The corner nearest the float's centre once the finger lets go. */
export function nearestFloatCorner(center: { x: number; y: number }, area: FloatArea): FloatCorner {
  const horizontal = center.x < (area.left + area.right) / 2 ? "left" : "right";
  const vertical = center.y < (area.top + area.bottom) / 2 ? "top" : "bottom";
  return `${vertical}-${horizontal}`;
}

/** The float's width as a share of the window: where it starts, what a double tap enlarges it to, and how far the person may take it. */
export const FLOAT_DEFAULT = 0.55;
export const FLOAT_LARGE = 0.8;
export const FLOAT_MIN = 0.3;
export const FLOAT_MAX = 0.9;

export function clampFloatFraction(value: number): number {
  return Math.min(FLOAT_MAX, Math.max(FLOAT_MIN, value));
}

/** The size the person left it at, or the default for anything that is not a share in range. */
export function parseFloatFraction(value: string | null): number {
  const parsed = value === null ? Number.NaN : Number(value);
  return Number.isFinite(parsed) && parsed >= FLOAT_MIN && parsed <= FLOAT_MAX ? parsed : FLOAT_DEFAULT;
}

/** A double tap goes between the default and the large size. */
export function toggledFloatFraction(fraction: number): number {
  return Math.abs(fraction - FLOAT_DEFAULT) < 0.01 ? FLOAT_LARGE : FLOAT_DEFAULT;
}

/**
 * The float's size: exactly the agent display's proportions, so its picture fills the window with
 * no bars, no stretching and no crop. The default stays under 60% of the window's height, a size
 * the person chose under 85%; a keyboard that takes room gives height first, down to the least width.
 */
export function floatSize(
  ratio: number,
  fraction: number,
  window: { width: number; height: number },
  freeHeight?: number,
) {
  const safe = ratio > 0 ? ratio : 9 / 20;
  const heightShare = Math.abs(fraction - FLOAT_DEFAULT) < 0.01 ? 0.6 : 0.85;
  let width = Math.min(fraction * window.width, heightShare * window.height * safe);
  if (freeHeight !== undefined) width = Math.min(width, freeHeight * safe);
  width = Math.max(width, FLOAT_MIN * window.width);
  return { width, height: width / safe };
}

/**
 * The width after a finger moved the corner opposite the float's own corner by (dx, dy): the
 * movement along the diagonal counts, so the proportions stay locked whichever way it is dragged.
 */
export function resizedFloatWidth(
  startWidth: number,
  ratio: number,
  delta: { x: number; y: number },
  corner: FloatCorner,
): number {
  const safe = ratio > 0 ? ratio : 9 / 20;
  const sx = corner.endsWith("left") ? 1 : -1;
  const sy = corner.startsWith("top") ? 1 : -1;
  return startWidth + (sx * delta.x + (sy * delta.y) / safe) / (1 + 1 / (safe * safe));
}
