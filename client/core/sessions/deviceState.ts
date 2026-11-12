/**
 * What this device remembers about Sessions, from the Expo app's
 * services/storage.ts, over a KeyValueStore. The Expo app's AsyncStorage
 * entries are copied into NSH storage on the upgrade (legacyStorage.ts), so
 * the keys and value formats below are exactly the Expo app's:
 *
 * | Key | Value |
 * |---|---|
 * | `mewla:agent_aliases` | JSON object, Session key → nickname (trimmed, non-empty). |
 * | `mewla:v1:collapsed_session_sections` | JSON array of folded section keys (`<serverId>::cwd::<dir>`, `::project::<name>`, `::fallback`); removed when empty. |
 * | `mewla:codex_render_modes` | JSON object, Session key → `"chat"` or `"terminal"`. |
 * | `mewla:recent_agent_opens` | JSON object, Session key → open time in ms; the newest 100. |
 * | `mewla:terminal_font_size` | The phones' terminal text size in points, a whole number 8–14, set by a pinch (new in the NSH app). |
 *
 * A Session key is `makeSessionKey(serverId, workerId)`, the JSON text
 * `["<serverId>","<workerId>"]` (sessionKeys.ts). A value that does not parse
 * reads as empty; entries of the wrong shape are dropped on read.
 */
import type { KeyValueStore } from "../platform";

export const DEVICE_STATE_KEYS = {
  recentWorkerOpens: "mewla:recent_agent_opens",
  workerAliases: "mewla:agent_aliases",
  collapsedSessionSections: "mewla:v1:collapsed_session_sections",
  interfaceRenderModes: "mewla:codex_render_modes",
  terminalFontSize: "mewla:terminal_font_size",
} as const;

const KEYS = DEVICE_STATE_KEYS;

export type StoredRecentWorkerOpens = Record<string, number>;
export type StoredWorkerAliases = Record<string, string>;
export type StoredInterfaceRenderMode = "chat" | "terminal";
export type StoredInterfaceRenderModes = Record<
  string,
  StoredInterfaceRenderMode
>;

export async function getRecentWorkerOpens(storage: KeyValueStore): Promise<StoredRecentWorkerOpens> {
  const value = await storage.getItem(KEYS.recentWorkerOpens);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredRecentWorkerOpens = {};
    for (const [workerId, openedAt] of Object.entries(parsed)) {
      if (typeof openedAt === "number" && Number.isFinite(openedAt)) {
        normalized[workerId] = openedAt;
      }
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function markWorkerOpened(
  storage: KeyValueStore,
  workerId: string,
  openedAt: number = Date.now(),
): Promise<void> {
  const current = await getRecentWorkerOpens(storage);
  const next: StoredRecentWorkerOpens = {
    ...current,
    [workerId]: openedAt,
  };

  const entries = Object.entries(next)
    .sort((left, right) => right[1] - left[1])
    .slice(0, 100);

  await storage.setItem(
    KEYS.recentWorkerOpens,
    JSON.stringify(Object.fromEntries(entries)),
  );
}

/** Keys of the Sessions directory sections the person folded; each key names its server. */
export async function getCollapsedSessionSections(storage: KeyValueStore): Promise<string[]> {
  const value = await storage.getItem(KEYS.collapsedSessionSections);
  if (!value) return [];
  try {
    const parsed = JSON.parse(value) as unknown;
    return Array.isArray(parsed)
      ? parsed.filter((key): key is string => typeof key === "string")
      : [];
  } catch {
    return [];
  }
}

export async function setCollapsedSessionSections(storage: KeyValueStore, keys: string[]): Promise<void> {
  if (keys.length === 0) {
    await storage.removeItem(KEYS.collapsedSessionSections);
    return;
  }
  await storage.setItem(KEYS.collapsedSessionSections, JSON.stringify(keys));
}

export async function getWorkerAliases(storage: KeyValueStore): Promise<StoredWorkerAliases> {
  const value = await storage.getItem(KEYS.workerAliases);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredWorkerAliases = {};
    for (const [workerId, alias] of Object.entries(parsed)) {
      if (typeof workerId !== "string" || workerId.trim().length === 0) continue;
      if (typeof alias !== "string") continue;

      const trimmed = alias.trim();
      if (!trimmed) continue;
      normalized[workerId] = trimmed;
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function setWorkerAlias(
  storage: KeyValueStore,
  workerId: string,
  alias: string,
): Promise<StoredWorkerAliases> {
  const current = await getWorkerAliases(storage);
  const next: StoredWorkerAliases = { ...current };
  const trimmed = alias.trim();

  if (trimmed) {
    next[workerId] = trimmed;
  } else {
    delete next[workerId];
  }

  await storage.setItem(KEYS.workerAliases, JSON.stringify(next));
  return next;
}

export async function getInterfaceRenderModes(storage: KeyValueStore): Promise<StoredInterfaceRenderModes> {
  const value = await storage.getItem(KEYS.interfaceRenderModes);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredInterfaceRenderModes = {};
    for (const [workerId, mode] of Object.entries(parsed)) {
      if (typeof workerId !== "string" || workerId.trim().length === 0) continue;
      if (mode === "chat" || mode === "terminal") {
        normalized[workerId] = mode;
      }
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function setInterfaceRenderMode(
  storage: KeyValueStore,
  workerId: string,
  mode: StoredInterfaceRenderMode,
): Promise<StoredInterfaceRenderModes> {
  const trimmed = workerId.trim();
  if (!trimmed) {
    return getInterfaceRenderModes(storage);
  }

  const current = await getInterfaceRenderModes(storage);
  const next: StoredInterfaceRenderModes = {
    ...current,
    [trimmed]: mode,
  };

  await storage.setItem(KEYS.interfaceRenderModes, JSON.stringify(next));
  return next;
}

/**
 * The phones' terminal text size (SPEC 11, Terminal): mono 10/14 until the person pinches; a pinch
 * sets 8–14, remembered on this device. Its line height is 1.4 times it, in whole points (10/14, 9/13).
 */
export const TERMINAL_FONT = { default: 10, min: 8, max: 14 } as const;

/** A stored or pinched size as one the terminal draws at: whole, within 8–14; anything else the default. */
export function terminalFontSize(value: unknown): number {
  const size = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
  if (typeof size !== "number" || !Number.isFinite(size)) return TERMINAL_FONT.default;
  return Math.min(TERMINAL_FONT.max, Math.max(TERMINAL_FONT.min, Math.round(size)));
}

export async function getTerminalFontSize(storage: KeyValueStore): Promise<number> {
  return terminalFontSize(await storage.getItem(KEYS.terminalFontSize));
}

export async function setTerminalFontSize(storage: KeyValueStore, size: number): Promise<number> {
  const next = terminalFontSize(size);
  await storage.setItem(KEYS.terminalFontSize, String(next));
  return next;
}
