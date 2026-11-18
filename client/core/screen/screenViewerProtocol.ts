// Messages between the Screen page and its viewer (runtime/viewer.js).

/** WebRTC signalling for the pushed-video picture, as the computer and the viewer page exchange it. */
export type ScreenSignal =
  | { type: "rtc"; sdp_type: "offer" | "answer"; sdp: string; ice_servers?: { urls: string[]; username?: string; credential?: string }[] }
  | { type: "ice"; candidate: Record<string, unknown> | null };

export type ScreenHostMessage =
  /** `video`: the socket asked for pushed video; the computer answers with an offer (or, when older, RFB). */
  | { type: "open"; video?: boolean }
  | { type: "signal"; signal: ScreenSignal }
  | { type: "close" }
  /** `at` is when the app handed the bytes over, for timing the bridge. */
  | { type: "rfb"; data: string[]; at: number }
  | { type: "text"; text: string; mods: ScreenModifier[] }
  | { type: "key"; key: ScreenKey; mods: ScreenModifier[] }
  | { type: "mode"; mode: ScreenPointerMode }
  /** Fit the picture to the view (the view's own zoom; the computer doesn't change). */
  | { type: "fit" }
  /** Web: the view goes full size, and the browser's own shortcuts go to the remote. */
  | { type: "fullsize" }
  /** Where a picture shorter than the page sits. */
  | { type: "align"; align: "top" | "center" };

export type ScreenModifier = "Command" | "Control" | "Option" | "Shift";
export type ScreenKey =
  | "Escape" | "Tab" | "Return" | "BackSpace" | "Delete"
  | "Left" | "Up" | "Right" | "Down" | "Home" | "End" | "PageUp" | "PageDown"
  | `F${number}`;
export type ScreenPointerMode = "touch" | "trackpad";

export type ScreenViewerMessage =
  | { type: "ready" }
  | { type: "send"; data: string }
  | { type: "signal"; signal: ScreenSignal }
  | { type: "closed" }
  | { type: "connected"; width: number; height: number }
  | { type: "disconnected"; clean: boolean }
  | { type: "name"; name: string }
  | { type: "zoom"; zoom: number }
  | { type: "haptic" }
  /** The view's size in CSS points and its pixels per point: on open and whenever it changes. */
  | { type: "size"; width: number; height: number; dpr: number }
  | {
      type: "stats";
      fps: number;
      kbps: number;
      inputMs: number | null;
      /** Median ms for bytes to cross from the app into the viewer. */
      bridgeMs?: number | null;
      /** Median ms from handing noVNC its bytes to the first redraw. */
      presentMs?: number | null;
      width: number;
      height: number;
    };

const VIEWER_TYPES = new Set(["ready", "send", "signal", "closed", "connected", "disconnected", "name", "zoom", "haptic", "stats", "size"]);

/** Parses a viewer message; anything unexpected is dropped. */
export function parseScreenViewerMessage(raw: string): ScreenViewerMessage | null {
  if (typeof raw !== "string" || raw.length > 1_000_000) return null;
  try {
    const value = JSON.parse(raw);
    if (!value || typeof value.type !== "string" || !VIEWER_TYPES.has(value.type)) return null;
    if (value.type === "send" && (typeof value.data !== "string" || !/^[A-Za-z0-9+/=]*$/.test(value.data))) return null;
    if (value.type === "signal" && !isSignal(value.signal)) return null;
    if (value.type === "size" && ![value.width, value.height, value.dpr].every((n) => typeof n === "number" && Number.isFinite(n) && n >= 0)) return null;
    return value as ScreenViewerMessage;
  } catch {
    return null;
  }
}

/** Whether a value is a signalling message (from the viewer page or the computer). */
export function isSignal(value: unknown): value is ScreenSignal {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  if (v.type === "rtc") return (v.sdp_type === "offer" || v.sdp_type === "answer") && typeof v.sdp === "string" && v.sdp.length < 64_000;
  if (v.type === "ice") return v.candidate === null || (typeof v.candidate === "object" && JSON.stringify(v.candidate).length < 4_000);
  return false;
}
