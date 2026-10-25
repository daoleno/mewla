// The cat's house is one web page the daemon owns. The phone cannot load it by
// URL (its connection is a pinned tunnel on a port that changes per launch, and
// the daemon only serves browsers on web UI origins), so the App syncs the
// daemon's bundle into a content-addressed cache with signed requests and loads
// it from there. This module is the pure part: the manifest, the cache plan and
// the bridge messages between the page and the App. The page holds no
// credentials and never touches the network; the App forwards its actions over
// the existing WebSocket.

export type CatKind =
  | "studio" | "harbour" | "library" | "workshop" | "night" | "garden"
  | "observatory" | "market" | "kitchen" | "postoffice" | "station" | "trail";

export interface CatPostcard {
  id: string;
  work_id?: string;
  kind?: CatKind;
  /** start and long come from a trip; welcome and letter are written at home. */
  phase: "start" | "long" | "welcome" | "letter";
  place?: string;
  act?: string;
  time?: string;
  weather?: string;
  friend?: string;
  treat?: string;
  hobby?: string;
  seed: number;
  at: number;
  seen?: boolean;
}

export interface CatSouvenir {
  id: string;
  work_id: string;
  kind: CatKind;
  item: string;
  title?: string;
  hobby?: string;
  at: number;
  seen?: boolean;
}

export interface CatYard {
  day: string;
  treats: { slot: number; kind: string; taken?: boolean }[];
  pantry: Record<string, number>;
  bag?: string;
}

/** The cat itself: its bond, hobbies, mood and what it remembers. */
export interface CatSelf {
  level: number;
  stars: number;
  need: number;
  hobbies: string[];
  pending: string[];
  mood: string;
  favorite?: string;
  fav_hobby?: string;
  today: Record<string, number>;
  learned: Record<string, boolean>;
  bowl?: string;
  gap: number;
  /** When the person was last here before this visit (ms): the start of "while you were away". */
  since?: number;
  /** What the person named it; it signs its letters with it. */
  name?: string;
  visits: number;
  memory: { at: number; what: string; arg?: string }[];
}

/** What the daemon says the cat is doing and has collected. */
export interface CatView {
  /** The Settings → Labs switch, kept on the computer: off, there is no Cat tab. */
  enabled: boolean;
  /** While the cat is out: where it went, when it is back (ms) and the postcard the outing sent. */
  presence: { away: boolean; waiting: boolean; kind?: CatKind; back?: number; card?: string };
  postcards: CatPostcard[];
  souvenirs: CatSouvenir[];
  yard: CatYard;
  cat: CatSelf;
  now: number;
}

export interface CatManifestFile {
  path: string;
  size: number;
  sha256: string;
}

export interface CatManifest {
  version: string;
  files: CatManifestFile[];
}

export const CAT_BUNDLE_PURPOSE = "mewla-cat";
export const CAT_INDEX = "index.html";
const MAX_FILES = 400;
const MAX_FILE_BYTES = 8 * 1024 * 1024;
const SAFE_PATH = /^[A-Za-z0-9_][A-Za-z0-9_.-]*(\/[A-Za-z0-9_][A-Za-z0-9_.-]*)*$/;

/** A bundle path stays inside the cache directory: relative, no dot segments. */
export function isSafeCatPath(path: unknown): path is string {
  return typeof path === "string" && path.length <= 120 && SAFE_PATH.test(path) && !path.split("/").some((part) => part === "..");
}

/** The daemon's manifest, or null when it is not a manifest this App can trust to write. */
export function parseCatManifest(value: unknown): CatManifest | null {
  if (typeof value !== "object" || value === null) return null;
  const record = value as { version?: unknown; files?: unknown };
  if (typeof record.version !== "string" || !/^[0-9a-f]{8,64}$/.test(record.version)) return null;
  if (!Array.isArray(record.files) || record.files.length === 0 || record.files.length > MAX_FILES) return null;
  const files: CatManifestFile[] = [];
  const seen = new Set<string>();
  for (const entry of record.files) {
    const file = entry as Partial<CatManifestFile> | null;
    if (!file || !isSafeCatPath(file.path) || seen.has(file.path)) return null;
    if (typeof file.size !== "number" || !Number.isInteger(file.size) || file.size < 0 || file.size > MAX_FILE_BYTES) return null;
    if (typeof file.sha256 !== "string" || !/^[0-9a-f]{64}$/.test(file.sha256)) return null;
    seen.add(file.path);
    files.push({ path: file.path, size: file.size, sha256: file.sha256 });
  }
  if (!seen.has(CAT_INDEX)) return null;
  return { version: record.version, files };
}

/** What still has to be fetched: files missing from the cache or of the wrong size. */
export function filesToFetch(manifest: CatManifest, cached: ReadonlyMap<string, number>): CatManifestFile[] {
  return manifest.files.filter((file) => cached.get(file.path) !== file.size);
}

/** Cache versions to delete: everything but the newest `keep`, never the one in use. */
export function staleVersions(versions: readonly string[], inUse: string, order: readonly string[], keep = 2): string[] {
  const recent = new Set([inUse, ...order.slice(0, keep)]);
  return versions.filter((version) => !recent.has(version));
}

/* ---- bridge between the page and the App ---- */

export type CatBridgeMessage =
  | { type: "ready" }
  | { type: "back" }
  | { type: "open_brain" }
  | { type: "act"; act: string; arg: string };

/** A message from the page, validated: the page is content, not authority. */
export function parseCatBridgeMessage(raw: unknown): CatBridgeMessage | null {
  let value: unknown = raw;
  if (typeof raw === "string") {
    if (raw.length > 4096) return null;
    try {
      value = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  if (typeof value !== "object" || value === null) return null;
  const message = value as Record<string, unknown>;
  if (message.cat !== 1) return null;
  switch (message.type) {
    case "ready":
    case "back":
    case "open_brain":
      return { type: message.type };
    case "act":
      // the daemon knows the acts and checks each one; here only the shape
      return typeof message.act === "string" && /^[a-z]{2,16}$/.test(message.act)
        && typeof message.arg === "string" && /^[A-Za-z0-9:_.-]{0,200}$/.test(message.arg)
        ? { type: "act", act: message.act, arg: message.arg }
        : null;
    default:
      return null;
  }
}

/** The WebSocket message a page action becomes, or null when it stays in the App. */
export function catSocketMessage(message: CatBridgeMessage): Record<string, unknown> | null {
  switch (message.type) {
    case "act":
      return { type: "cat_act", act: message.act, arg: message.arg };
    default:
      return null;
  }
}

const LINE_SEPARATOR = String.fromCharCode(0x2028);
const PARAGRAPH_SEPARATOR = String.fromCharCode(0x2029);

/**
 * Script that hands a state to the page; U+2028/9 are escaped so the literal is
 * always valid. `tab` tells the page the App's header already leads home.
 */
export function catStateScript(view: CatView, tab?: boolean): string {
  const payload = JSON.stringify({ cat: 1, type: "state", state: view, tab })
    .split(LINE_SEPARATOR).join("\\u2028")
    .split(PARAGRAPH_SEPARATOR).join("\\u2029");
  return `window.__catHost&&window.__catHost(${payload});true;`;
}

/**
 * Tells the page whether the tab is on screen and the App in front: the room
 * rests and its sounds stop while it is not.
 */
export function catFocusScript(on: boolean): string {
  return `window.__catHost&&window.__catHost(${JSON.stringify({ cat: 1, type: "focus", on })});true;`;
}

/** Cached last-known state is per computer: another computer has another cat. */
export function catCacheKey(serverKey: string): string {
  return `mewla.cat.view.v2:${serverKey}`;
}

/** Where the App remembers each computer's Labs switch, so the tab does not flicker on launch. */
export function catLabsKey(serverKey: string): string {
  return `mewla.cat.labs.v1:${serverKey}`;
}

export function parseCatView(raw: unknown): CatView | null {
  if (typeof raw !== "object" || raw === null) return null;
  const view = raw as Partial<CatView>;
  if (!view.presence || typeof view.presence !== "object" || !Array.isArray(view.postcards) || !Array.isArray(view.souvenirs) || !view.yard || !view.cat) return null;
  return { ...view, enabled: view.enabled === true } as CatView;
}
