import type { ScreenAgent, ScreenPower, ScreenStatus } from "./screen";
import { PATH_LABELS } from "./screenStream";
import type { ScreenPath, ScreenStreamStats } from "./screenStream";
import type { ScreenModifier } from "./screenViewerProtocol";

/**
 * The hidden text field always holds this, so a soft keyboard's backspace
 * has something to delete and every edit is visible as a diff.
 */
export const KEYBOARD_SENTINEL = "  ";

/**
 * Typing on a remote screen while the soft keyboard is up: Android's Back hides the keyboard
 * without blurring the hidden field, so typing (the key bar, the tinted button) stayed on (screen
 * [69]). One step per keyboard change: `seen` is whether the keyboard came up during this typing;
 * `end` says to stop typing because it went down after that. A keyboard not up yet is no hide.
 */
export function typingKeyboardStep(seen: boolean, typing: boolean, keyboardUp: boolean): { seen: boolean; end: boolean } {
  if (!typing) return { seen: false, end: false };
  if (keyboardUp) return { seen: true, end: false };
  return { seen: false, end: seen };
}

/** One soft-keyboard change of the hidden field, as keystrokes: this many backspaces, then this text. */
export interface KeyboardEdit {
  backspace: number;
  text: string;
}

/** How long typing pauses before the field is put back to the sentinel. */
export const FIELD_RESET_MS = 400;

/**
 * One change of the hidden field, against what it held before (as the field reported it, not as it was
 * last set): the characters after the common start were deleted, then the new ones typed. A key that
 * lands before a reset of the field reaches it extends what the field held, so it is never sent twice
 * (screen: "xyz" came out "xyyz"); a keyboard replacing a composed word deletes it and types the word.
 */
export function keyboardEdit(prev: string, next: string): KeyboardEdit {
  const a = Array.from(prev);
  const b = Array.from(next);
  let same = 0;
  while (same < a.length && same < b.length && a[same] === b[same]) same++;
  return { backspace: a.length - same, text: b.slice(same).join("") };
}

/** Whether the field must be put back now: nearly empty, a backspace would have nothing to delete. */
export function fieldNeedsReset(next: string): boolean {
  return Array.from(next).length < KEYBOARD_SENTINEL.length;
}

export interface ModifierState {
  /** Applied to the next key, then cleared. */
  armed: ScreenModifier[];
  /** Held until tapped again. */
  locked: ScreenModifier[];
}

export const NO_MODIFIERS: ModifierState = { armed: [], locked: [] };

/** Tap arms a modifier for one key; a second tap locks it; a third clears it. */
export function tapModifier(state: ModifierState, modifier: ScreenModifier): ModifierState {
  if (state.locked.includes(modifier)) return { ...state, locked: state.locked.filter((m) => m !== modifier) };
  if (state.armed.includes(modifier)) {
    return { armed: state.armed.filter((m) => m !== modifier), locked: [...state.locked, modifier] };
  }
  return { ...state, armed: [...state.armed, modifier] };
}

export function activeModifiers(state: ModifierState): ScreenModifier[] {
  return [...new Set([...state.locked, ...state.armed])];
}

/** After a key is sent, armed modifiers are spent; locked ones stay. */
export function spendModifiers(state: ModifierState): ModifierState {
  return state.armed.length ? { ...state, armed: [] } : state;
}

export function powerSummary(power: ScreenPower | undefined, keepAwake: boolean): string | null {
  if (!power) return null;
  const parts: string[] = [];
  if (power.source === "ac") parts.push(power.percent ? `Charger · ${power.percent}%` : "Charger");
  else if (power.source === "battery") parts.push(`Battery · ${power.percent ?? "?"}%`);
  parts.push(power.keeping_awake ? "Staying awake" : keepAwake ? "Starting keep-awake" : "May sleep");
  if (power.display_kept_for_agents) parts.push("Display kept on for agents");
  return parts.join(" · ");
}

/**
 * What to say when this computer can't share its screen. Macs, Windows and
 * Linux computers have Screen, so one that can't start gives its own reason;
 * anywhere else the way forward is another computer, which the app can
 * switch to; a browser stays bound to the computer that served it. The title
 * is the state; a detail appears only where there is something to act on or
 * the computer gave its own reason.
 */
export function unsupportedScreen(
  status: ScreenStatus,
  canSwitchComputer: boolean,
): { title: string; detail: string | null; canChooseComputer: boolean } {
  if (status.platform === "darwin" || status.platform === "windows" || status.platform === "linux") {
    const what = status.platform === "darwin" ? "Mac" : "computer";
    return { title: `Screen can't start on this ${what}`, detail: status.reason || null, canChooseComputer: false };
  }
  return {
    title: "Screen needs a Mac, Windows or Linux computer",
    detail: canSwitchComputer ? null : "Open Mewla from a Mac, Windows or Linux computer.",
    canChooseComputer: canSwitchComputer,
  };
}

/**
 * The one power risk worth a pill on the viewer, if any. The label is the
 * state, or the action when the pill itself fixes it.
 */
export function powerWarning(status: ScreenStatus | null): { label: string; action: "keep-awake" | null } | null {
  const power = status?.power;
  if (!status || !power || !status.approved) return null;
  if (power.lid_closed) return { label: "Lid closed", action: null };
  if (!status.keep_awake) return { label: "Keep awake", action: "keep-awake" };
  if (power.source === "battery") return { label: "On battery", action: null };
  return null;
}

export interface StreamReadout {
  rttMs: number | null;
  inputMs: number | null;
  fps: number;
  kbps: number;
  /** What the computer measured of the stream. */
  daemon?: ScreenStreamStats | null;
  /**
   * The path this phone chose, which it knows better than the daemon: the
   * daemon sees only the address a tunnel or proxy connected from.
   */
  viaPath?: ScreenPath | null;
  /**
   * Why a relay stream is not direct. "announced" means the computer listed
   * LAN or Tailscale addresses this phone could not reach (not on that Wi-Fi
   * or tailnet); "none" means it listed no address at all.
   */
  directHint?: "announced" | "none" | null;
}

export type StreamStage = "network" | "encode";

const STAGE_LABELS: Record<StreamStage, string> = {
  network: "network",
  encode: "encode",
};

/**
 * The stage that takes longest between the screen changing and your phone
 * showing it: the round trip (which includes bytes waiting in the link) or
 * the computer's encode. Null until something is measured.
 */
export function slowestStage(readout: StreamReadout): { stage: StreamStage; label: string; ms: number } | null {
  const stages: [StreamStage, number | null | undefined][] = [
    ["network", readout.rttMs],
    ["encode", readout.daemon && readout.daemon.updates > 0 ? readout.daemon.encodeMs : null],
  ];
  let best: { stage: StreamStage; label: string; ms: number } | null = null;
  for (const [stage, ms] of stages) {
    if (typeof ms !== "number" || !Number.isFinite(ms) || ms <= 0) continue;
    if (!best || ms > best.ms) best = { stage, label: STAGE_LABELS[stage], ms };
  }
  return best;
}

/** Round trip (ms) from which the network is the problem, not a number to read. */
export const SLOW_NETWORK_MS = 200;
/** Consecutive slow samples before a slow connection is worth saying. */
export const SLOW_SAMPLES = 3;

/**
 * A slow connection worth saying, in plain words, with the way out when the
 * phone has one. Everything else about the stream is a diagnostic. The fix
 * is offered only where the computer lists private addresses this phone could
 * not reach; with none listed, nothing on the phone changes that.
 */
export function connectionProblem(readout: StreamReadout): { title: string; fix: string | null } | null {
  if ((readout.rttMs ?? 0) < SLOW_NETWORK_MS) return null;
  const path = readout.viaPath ?? readout.daemon?.path;
  const fix = path === "relay" && readout.directHint === "announced" ? "Join your computer's Wi-Fi or Tailscale" : null;
  return { title: "Slow connection", fix };
}

/** The streak of slow samples after one more: a good one ends it. */
export function nextSlowStreak(streak: number, slow: boolean): number {
  return slow ? streak + 1 : 0;
}

/** True once slowness has lasted, so a single spike never lights the indicator. */
export function slowLasts(streak: number): boolean {
  return streak >= SLOW_SAMPLES;
}

export type IndicatorTone = "quiet" | "warn";

/**
 * The connection indicator: quiet unless something needs attention, either a
 * slow connection that lasts or a power risk the person can act on.
 */
export function indicatorTone(slow: boolean, warning: ReturnType<typeof powerWarning>): IndicatorTone {
  return slow || (warning !== null && warning.label !== "On battery") ? "warn" : "quiet";
}

export interface DiagnosticRow {
  label: string;
  value: string;
}

/** The diagnostics behind the indicator: every number once, in plain labels. */
export function diagnosticRows(readout: StreamReadout, power: string | null): DiagnosticRow[] {
  const rows: DiagnosticRow[] = [];
  const path = readout.viaPath ?? readout.daemon?.path;
  if (path) rows.push({ label: "Route", value: PATH_LABELS[path] });
  if (readout.rttMs !== null) rows.push({ label: "Network", value: `${Math.round(readout.rttMs)} ms` });
  if (readout.inputMs !== null) rows.push({ label: "To screen", value: `${Math.round(readout.inputMs)} ms` });
  rows.push({ label: "Frame rate", value: `${readout.fps < 10 ? readout.fps.toFixed(1) : Math.round(readout.fps)} fps` });
  rows.push({ label: "Data", value: readout.kbps >= 1000 ? `${(readout.kbps / 1000).toFixed(1)} Mb/s` : `${Math.round(readout.kbps)} kb/s` });
  const slowest = slowestStage(readout);
  if (slowest && slowest.stage !== "network") rows.push({ label: "Slowest", value: `${slowest.label} ${Math.round(slowest.ms)} ms` });
  if (power) rows.push({ label: "Computer", value: power });
  return rows;
}

/** What a Screen page shows for a computer's own screen, once it can be opened. */
export type ScreenEntry = "setup" | "local" | "agent" | "live";

/** The agents using the computer's screen now. */
export function screenAgents(status: Pick<ScreenStatus, "agent" | "agents"> | null | undefined): ScreenAgent[] {
  if (!status) return [];
  return status.agents?.length ? status.agents : status.agent ? [status.agent] : [];
}

/**
 * What Screen does with a computer's status. A device on the computer itself looks at the very screen the
 * viewer would stream, so a live view is a mirror of itself: it never opens one (no capture stream either),
 * and shows which agents use the screen with Stop instead. Elsewhere an agent at work waits for the person
 * to choose Watch or Take over, and a free screen opens.
 */
export function screenEntry(status: ScreenStatus, how: "auto" | "takeOver" | "watch" = "auto"): ScreenEntry {
  if (!status.ready) return "setup";
  if (status.local) return "local";
  if (how === "auto" && screenAgents(status).length > 0) return "agent";
  return "live";
}

/** The agents of the computer's screen that float as a live view: none on that computer itself, which would mirror it. */
export function floatedScreenAgents(status: ScreenStatus): ScreenAgent[] {
  return status.ready && !status.local ? screenAgents(status) : [];
}
