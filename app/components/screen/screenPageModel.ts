import type { ScreenPower, ScreenStatus } from "../../services/screen";
import { PATH_LABELS } from "../../services/connectionPath";
import type { ScreenPath, ScreenStreamStats } from "../../services/screenStream";
import type { ScreenModifier } from "./screenViewerProtocol";

/**
 * The hidden text field always holds this, so a soft keyboard's backspace
 * has something to delete and every edit is visible as a diff.
 */
export const KEYBOARD_SENTINEL = "  ";

export type KeyboardEdit =
  | { kind: "text"; text: string }
  | { kind: "backspace"; count: number }
  | { kind: "none" };

/** Turns one soft-keyboard edit of the sentinel field into keystrokes. */
export function keyboardEdit(next: string): KeyboardEdit {
  if (next === KEYBOARD_SENTINEL) return { kind: "none" };
  if (next.length < KEYBOARD_SENTINEL.length && KEYBOARD_SENTINEL.startsWith(next)) {
    return { kind: "backspace", count: KEYBOARD_SENTINEL.length - next.length };
  }
  if (next.startsWith(KEYBOARD_SENTINEL)) return { kind: "text", text: next.slice(KEYBOARD_SENTINEL.length) };
  // The cursor moved inside the sentinel or the keyboard replaced it; keep
  // only what the user typed.
  const text = next.replace(/ /g, "");
  return text ? { kind: "text", text } : { kind: "backspace", count: 1 };
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
 * What to say when this computer can't share its screen. Screen works only
 * on Macs for now, so anywhere else the way forward is the Mewla on a Mac,
 * which the app can switch to; a browser stays bound to the computer that
 * served it. The title is the state; a detail appears only where there is
 * something to act on or the computer gave its own reason.
 */
export function unsupportedScreen(
  status: ScreenStatus,
  canSwitchComputer: boolean,
): { title: string; detail: string | null; canChooseComputer: boolean } {
  if (status.platform === "darwin") {
    return { title: "Screen can't start on this Mac", detail: status.reason || null, canChooseComputer: false };
  }
  return {
    title: "Screen needs a Mac",
    detail: canSwitchComputer ? null : "Open Mewla from a Mac.",
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
  /** The viewer's own stages, from the page that draws. */
  bridgeMs?: number | null;
  presentMs?: number | null;
  /** The daemon's stages; absent from a daemon that predates them. */
  daemon?: ScreenStreamStats | null;
  /** The JPEG level the viewer asked for, 0-9. */
  viewerQuality?: number;
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

export type StreamStage = "network" | "encode" | "bridge" | "draw";

const STAGE_LABELS: Record<StreamStage, string> = {
  network: "network",
  encode: "encode",
  bridge: "app",
  draw: "draw",
};

/**
 * The stage that takes longest between the screen changing and your phone
 * showing it. They don't overlap: the round trip (which includes bytes
 * waiting in the link), the daemon's encode, the hop into the viewer page,
 * and the viewer's decode and draw. Null until something is measured.
 */
export function slowestStage(readout: StreamReadout): { stage: StreamStage; label: string; ms: number } | null {
  const stages: [StreamStage, number | null | undefined][] = [
    ["network", readout.rttMs],
    ["encode", readout.daemon && readout.daemon.updates > 0 ? readout.daemon.encodeMs : null],
    ["bridge", readout.bridgeMs],
    ["draw", readout.presentMs],
  ];
  let best: { stage: StreamStage; label: string; ms: number } | null = null;
  for (const [stage, ms] of stages) {
    if (typeof ms !== "number" || !Number.isFinite(ms) || ms <= 0) continue;
    if (!best || ms > best.ms) best = { stage, label: STAGE_LABELS[stage], ms };
  }
  return best;
}

/** True while adaptive streaming holds the JPEG level below what the viewer asked for. */
export function qualityReduced(daemon: ScreenStreamStats | null | undefined, viewerQuality = 6): boolean {
  return !!daemon && daemon.quality > 0 && daemon.quality < viewerQuality;
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
  const slow = (readout.rttMs ?? 0) >= SLOW_NETWORK_MS || qualityReduced(readout.daemon, readout.viewerQuality);
  if (!slow) return null;
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
  if (path) rows.push({ label: "Route", value: (readout.daemon?.video ?? 0) > 0 ? `${PATH_LABELS[path]} · video` : PATH_LABELS[path] });
  if (readout.rttMs !== null) rows.push({ label: "Network", value: `${Math.round(readout.rttMs)} ms` });
  if (readout.inputMs !== null) rows.push({ label: "To screen", value: `${Math.round(readout.inputMs)} ms` });
  rows.push({ label: "Frame rate", value: `${readout.fps < 10 ? readout.fps.toFixed(1) : Math.round(readout.fps)} fps` });
  rows.push({ label: "Data", value: readout.kbps >= 1000 ? `${(readout.kbps / 1000).toFixed(1)} Mb/s` : `${Math.round(readout.kbps)} kb/s` });
  if (qualityReduced(readout.daemon, readout.viewerQuality)) rows.push({ label: "Quality", value: "Reduced" });
  const slowest = slowestStage(readout);
  if (slowest && slowest.stage !== "network") rows.push({ label: "Slowest", value: `${slowest.label} ${Math.round(slowest.ms)} ms` });
  if (power) rows.push({ label: "Computer", value: power });
  return rows;
}
