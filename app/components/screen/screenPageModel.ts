import type { ScreenPower, ScreenStatus } from "../../services/screen";
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
  return parts.join(" · ");
}

/**
 * What to say when this computer can't share its screen. Screen works only
 * on Macs for now, so anywhere else the way forward is the Mewla on a Mac,
 * which the app can switch to; a browser stays bound to the computer that
 * served it.
 */
export function unsupportedScreen(
  status: ScreenStatus,
  name: string,
  canSwitchComputer: boolean,
): { title: string; detail: string; canChooseComputer: boolean } {
  if (status.platform === "darwin") {
    return { title: "Screen can't start on this Mac", detail: status.reason || `${name} can't share its screen with Mewla.`, canChooseComputer: false };
  }
  return {
    title: "Screen works on Macs for now",
    detail: canSwitchComputer
      ? `${name} isn't a Mac. To see and control a Mac, add the Mewla running on it and switch to that computer.`
      : `${name} isn't a Mac. To see and control a Mac, open Mewla on your phone and switch to the Mewla running on that Mac.`,
    canChooseComputer: canSwitchComputer,
  };
}

/** The one power risk worth interrupting the user for, if any. */
export function powerWarning(status: ScreenStatus | null): { title: string; detail: string } | null {
  const power = status?.power;
  if (!status || !power || !status.approved) return null;
  if (power.lid_closed) {
    return {
      title: "The lid is closed",
      detail: "A laptop with its lid closed sleeps unless an external display is connected. Keep the lid open while you're away.",
    };
  }
  if (!status.keep_awake) {
    return { title: "This computer may sleep", detail: "Once it sleeps you can't reach it until someone wakes it. Turn on Keep awake." };
  }
  if (power.source === "battery") {
    return { title: "On battery", detail: "It stays awake while idle, but connect the charger before you leave so it can't run down." };
  }
  return null;
}

export function formatStats(stats: { rttMs: number | null; inputMs: number | null; fps: number; kbps: number }): string {
  const parts: string[] = [];
  if (stats.rttMs !== null) parts.push(`${Math.round(stats.rttMs)} ms network`);
  if (stats.inputMs !== null) parts.push(`${Math.round(stats.inputMs)} ms to screen`);
  parts.push(`${stats.fps < 10 ? stats.fps.toFixed(1) : Math.round(stats.fps)} fps`);
  parts.push(stats.kbps >= 1000 ? `${(stats.kbps / 1000).toFixed(1)} Mb/s` : `${Math.round(stats.kbps)} kb/s`);
  return parts.join(" · ");
}

/** Connection quality from network round trip, for the status dot. */
export function connectionQuality(rttMs: number | null): "good" | "fair" | "poor" | "unknown" {
  if (rttMs === null) return "unknown";
  if (rttMs < 80) return "good";
  if (rttMs < 200) return "fair";
  return "poor";
}
