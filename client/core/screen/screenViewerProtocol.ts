// Messages between the Screen page and its viewer (runtime/viewer.js).

/** WebRTC signalling for the pushed-video picture, as the computer and the viewer page exchange it. */
export type ScreenSignal =
  | {
      type: "rtc";
      sdp_type: "offer" | "answer";
      sdp: string;
      ice_servers?: { urls: string[]; username?: string; credential?: string }[];
      /** On the offer: the computer takes scrolls by picture pixels (client message 250), not only wheel notches. */
      fine_scroll?: boolean;
      /** On the offer: the video carries the computer's own cursor, so the viewer draws no arrow of its own. */
      cursor?: boolean;
      /** On the offer: the computer's signature over its DTLS fingerprints (screenIdentity). */
      assertion?: { timestamp?: unknown; nonce_hex?: unknown; signature_hex?: unknown };
      /** On the answer: this device's signature over its DTLS fingerprints. */
      authorization?: string;
    }
  | { type: "ice"; candidate: Record<string, unknown> | null };

export type ScreenHostMessage =
  /** The socket is open: the computer's offer follows. */
  | { type: "open" }
  | { type: "signal"; signal: ScreenSignal }
  | { type: "close" }
  | { type: "text"; text: string; mods: ScreenModifier[] }
  | { type: "key"; key: ScreenKey; mods: ScreenModifier[] }
  /** The picture is a window laid out for this view (a reflowed agent browser): used like a phone's page, no pointer drawn. */
  | { type: "layout"; reflowed: boolean }
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

export type ScreenViewerMessage =
  | { type: "ready" }
  | { type: "signal"; signal: ScreenSignal }
  | { type: "connected"; width: number; height: number }
  | { type: "disconnected"; clean: boolean }
  | { type: "zoom"; zoom: number }
  | { type: "haptic" }
  /** The view's size in CSS points and its pixels per point: on open and whenever it changes. */
  | { type: "size"; width: number; height: number; dpr: number }
  | {
      type: "stats";
      fps: number;
      kbps: number;
      inputMs: number | null;
      width: number;
      height: number;
    };

const VIEWER_TYPES = new Set(["ready", "signal", "connected", "disconnected", "zoom", "haptic", "stats", "size"]);

/** Parses a viewer message; anything unexpected is dropped. */
export function parseScreenViewerMessage(raw: string): ScreenViewerMessage | null {
  if (typeof raw !== "string" || raw.length > 1_000_000) return null;
  try {
    const value = JSON.parse(raw);
    if (!value || typeof value.type !== "string" || !VIEWER_TYPES.has(value.type)) return null;
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
