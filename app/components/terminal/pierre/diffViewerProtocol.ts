// Messages between the Git review sheet and its diff reader (runtime/diffs.js).

/** CSS custom properties the reader's theme reads, set on its container. */
export type DiffViewerVars = Record<string, string>;

export interface DiffViewerSection {
  /** Unique within one `diff` message; prefixes the reader's item ids. */
  id: string;
  /** Shown before the file name when one file has several sections. */
  label?: string;
  patch: string;
  /** The host can supply both full sides, so collapsed context can expand. */
  expandable: boolean;
}

export type DiffHostMessage =
  | { type: "style"; vars: DiffViewerVars; dark: boolean; fontFamily: string; fontSize: number }
  /** `inset` is extra space under the last line, for the device's bottom safe area. */
  | { type: "layout"; split: boolean; wrap: boolean; headers: boolean; inset: number }
  /** `key` names the comparison; the reader restores its scroll position per key. */
  | { type: "diff"; key: string; sections: DiffViewerSection[] }
  | { type: "file"; key: string; path: string; contents: string }
  | { type: "clear" }
  /** Answer to `needFiles`; null on both sides means the sides are unavailable. */
  | { type: "files"; id: string; oldContents: string | null; newContents: string | null; oldName?: string }
  | { type: "find"; query: string }
  | { type: "seek"; direction: "next" | "previous" }
  /** Highlights a line range (a hunk, say); `section: null` clears it. Never echoed back. */
  | { type: "select"; section: string | null; side: "additions" | "deletions"; start: number; end: number; scroll: boolean }
  | { type: "top" };

export type DiffViewerMessage =
  | { type: "ready" }
  | { type: "loaded"; key: string; hunks: number; ms: number }
  | { type: "needFiles"; id: string }
  | { type: "find"; query: string; count: number; index: number }
  /** A tap selected lines; `section` is the DiffViewerSection id, null when cleared. */
  | { type: "selection"; section: string | null; side: "additions" | "deletions"; start: number; end: number }
  | { type: "error"; key?: string; message: string };

export interface DiffViewerHostProps {
  onMessage(raw: string): void;
  backgroundColor: string;
  accessibilityLabel: string;
}

export interface DiffViewerHostHandle {
  post(batch: DiffHostMessage[]): void;
}

const VIEWER_TYPES = new Set(["ready", "loaded", "needFiles", "find", "selection", "error"]);
const MAX_MESSAGE_BYTES = 100_000;

/** Parses a reader message; anything unexpected is dropped. */
export function parseDiffViewerMessage(raw: string): DiffViewerMessage | null {
  if (typeof raw !== "string" || raw.length > MAX_MESSAGE_BYTES) return null;
  try {
    const value = JSON.parse(raw);
    if (!value || typeof value.type !== "string" || !VIEWER_TYPES.has(value.type)) return null;
    switch (value.type) {
      case "loaded":
        if (typeof value.key !== "string" || !Number.isFinite(value.hunks) || !Number.isFinite(value.ms)) return null;
        break;
      case "needFiles":
        if (typeof value.id !== "string") return null;
        break;
      case "find":
        if (typeof value.query !== "string" || !Number.isInteger(value.count) || !Number.isInteger(value.index)) return null;
        break;
      case "selection":
        if (value.section !== null && typeof value.section !== "string") return null;
        if (value.side !== "additions" && value.side !== "deletions") return null;
        if (!Number.isInteger(value.start) || !Number.isInteger(value.end)) return null;
        break;
      case "error":
        if (typeof value.message !== "string") return null;
        break;
    }
    return value as DiffViewerMessage;
  } catch {
    return null;
  }
}
