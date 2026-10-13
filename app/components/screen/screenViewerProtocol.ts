// Messages between the Screen page and its viewer (runtime/viewer.js).

export type ScreenHostMessage =
  | { type: "open" }
  | { type: "close" }
  | { type: "rfb"; data: string[] }
  | { type: "text"; text: string; mods: ScreenModifier[] }
  | { type: "key"; key: ScreenKey; mods: ScreenModifier[] }
  | { type: "mode"; mode: ScreenPointerMode }
  | { type: "fit" };

export type ScreenModifier = "Command" | "Control" | "Option" | "Shift";
export type ScreenKey =
  | "Escape" | "Tab" | "Return" | "BackSpace" | "Delete"
  | "Left" | "Up" | "Right" | "Down" | "Home" | "End" | "PageUp" | "PageDown"
  | `F${number}`;
export type ScreenPointerMode = "touch" | "trackpad";

export type ScreenViewerMessage =
  | { type: "ready" }
  | { type: "send"; data: string }
  | { type: "closed" }
  | { type: "connected"; width: number; height: number }
  | { type: "disconnected"; clean: boolean }
  | { type: "name"; name: string }
  | { type: "zoom"; zoom: number }
  | { type: "haptic" }
  | { type: "stats"; fps: number; kbps: number; inputMs: number | null; width: number; height: number };

export interface ScreenViewerHostProps {
  onMessage(raw: string): void;
  backgroundColor: string;
  accessibilityLabel: string;
}

export interface ScreenViewerHostHandle {
  post(batch: ScreenHostMessage[]): void;
}

const VIEWER_TYPES = new Set(["ready", "send", "closed", "connected", "disconnected", "name", "zoom", "haptic", "stats"]);

/** Parses a viewer message; anything unexpected is dropped. */
export function parseScreenViewerMessage(raw: string): ScreenViewerMessage | null {
  if (typeof raw !== "string" || raw.length > 1_000_000) return null;
  try {
    const value = JSON.parse(raw);
    if (!value || typeof value.type !== "string" || !VIEWER_TYPES.has(value.type)) return null;
    if (value.type === "send" && (typeof value.data !== "string" || !/^[A-Za-z0-9+/=]*$/.test(value.data))) return null;
    return value as ScreenViewerMessage;
  } catch {
    return null;
  }
}
