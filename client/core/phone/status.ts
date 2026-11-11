/**
 * Phone use on this phone, as the Android host reports it (`phone.status` and the `phone.status`
 * event; client/android/phone PhoneController.status). Only the GitHub APK on Android has it.
 */

/** Where the guided setup is: the step, what only the person can do, and whether it has stopped getting anywhere. */
export interface PhoneGuide {
  step: "developer" | "wireless" | "pair" | null;
  /** "allow": the system's prompt for this network; "lock": the screen lock. */
  needs: "allow" | "lock" | null;
  stalled: boolean;
}

/** The word the computer is told: ready, setup, person_in_control or stopped. */
export type PhoneState = "ready" | "setup" | "person_in_control" | "stopped";

/** The link to the current computer: off (Phone use off, or no computer), connecting, online, offline, replaced, unsupported. */
export type PhoneLinkState = "off" | "connecting" | "online" | "offline" | "replaced" | "unsupported";

/** What the helper and the phone's settings say, as the setup screen reads it live. */
export interface PhoneStatus {
  supported: boolean;
  sdk: number;
  enabled: boolean;
  /** The link to the computer is up. */
  link: boolean;
  link_state: PhoneLinkState;
  developer_options: boolean;
  wireless_debugging: boolean;
  wifi: boolean;
  paired: boolean;
  helper: boolean;
  /** Why the helper is not up (what its last start attempt ran into), or null. */
  problem: string | null;
  accessibility: boolean;
  /** Android may block switching the accessibility service on until "Allow restricted settings" is chosen in App info (Android 13+, not installed from the Play Store). */
  restricted: boolean;
  /** The App may write secure settings (granted once through its helper): it restores Wireless debugging and its own accessibility service itself. */
  self_healing: boolean;
  /** The guided setup while it walks Settings, else null. */
  guide: PhoneGuide | null;
  notifications: boolean;
  state: PhoneState;
  /** The agent's display size; 0 until the display exists. */
  w: number;
  h: number;
  /** The display's density as asked and as the system has it, next to the phone's own screen; null until the display exists. */
  display_info: string | null;
  /** The agent working now, or null. */
  agent: string | null;
  /** What the agent asked the person to do, or null. */
  handoff: string | null;
  /** The agent's display has shown a picture since it was made; until then there is nothing to show. */
  drawn: boolean;
  /** The app being opened on the agent's display now, or null. */
  opening: string | null;
  /** The app the last launch could not open, or null. */
  launch_failed: string | null;
}

const STATES: readonly PhoneState[] = ["ready", "setup", "person_in_control", "stopped"];
const LINKS: readonly PhoneLinkState[] = ["off", "connecting", "online", "offline", "replaced", "unsupported"];

const text = (value: unknown): string | null => (typeof value === "string" && value ? value : null);
const flag = (value: unknown): boolean => value === true;
const count = (value: unknown): number => (typeof value === "number" && Number.isFinite(value) ? value : 0);

function guideOf(value: unknown): PhoneGuide | null {
  if (!value || typeof value !== "object") return null;
  const g = value as Record<string, unknown>;
  const step = g.step === "developer" || g.step === "wireless" || g.step === "pair" ? g.step : null;
  const needs = g.needs === "allow" || g.needs === "lock" ? g.needs : null;
  return { step, needs, stalled: flag(g.stalled) };
}

/** The host's status, every field checked; null when it is not one. */
export function parsePhoneStatus(value: unknown): PhoneStatus | null {
  if (!value || typeof value !== "object") return null;
  const s = value as Record<string, unknown>;
  if (typeof s.supported !== "boolean" || typeof s.enabled !== "boolean") return null;
  const state = STATES.includes(s.state as PhoneState) ? (s.state as PhoneState) : "setup";
  const link = flag(s.link);
  const linkState = LINKS.includes(s.link_state as PhoneLinkState) ? (s.link_state as PhoneLinkState) : link ? "online" : "off";
  return {
    supported: s.supported,
    sdk: count(s.sdk),
    enabled: s.enabled,
    link,
    link_state: linkState,
    developer_options: flag(s.developer_options),
    wireless_debugging: flag(s.wireless_debugging),
    wifi: flag(s.wifi),
    paired: flag(s.paired),
    helper: flag(s.helper),
    problem: text(s.problem),
    accessibility: flag(s.accessibility),
    restricted: flag(s.restricted),
    self_healing: flag(s.self_healing),
    guide: guideOf(s.guide),
    notifications: flag(s.notifications),
    state,
    w: count(s.w),
    h: count(s.h),
    display_info: text(s.display_info),
    agent: text(s.agent),
    handoff: text(s.handoff),
    drawn: flag(s.drawn),
    opening: text(s.opening),
    launch_failed: text(s.launch_failed),
  };
}
