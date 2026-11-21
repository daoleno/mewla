import { DEVICE_SECURE_KEYS } from "../auth";
import type { KeyValueStore } from "../platform";
import { SERVER_KEYS } from "../servers";

/**
 * Moving to another app (a release of another application id installs beside the running app): what makes this
 * phone a paired device of its computers, in one piece. The old app stages it where the new one reads it (Android's
 * handover provider, signature-protected so only an app of the same release key can ask); the new app takes it once
 * into an empty store and says so, and the old app forgets it.
 */
export interface HandoverBundle {
  version: 1;
  /** The device key: id, name, seed and public key. */
  secure: Record<string, string>;
  /** The computers it is paired with, which one is current. */
  storage: Record<string, string>;
}

const STORAGE_KEYS = [SERVER_KEYS.servers, SERVER_KEYS.currentServer, SERVER_KEYS.disabledServers, SERVER_KEYS.onboarded] as const;

/** What to hand over; null when this app was never paired (no device key, no computer): nothing to move. */
export async function collectHandover(secure: KeyValueStore, storage: KeyValueStore): Promise<HandoverBundle | null> {
  const bundle: HandoverBundle = { version: 1, secure: {}, storage: {} };
  for (const key of DEVICE_SECURE_KEYS) {
    const value = await secure.getItem(key);
    if (value) bundle.secure[key] = value;
  }
  for (const key of STORAGE_KEYS) {
    const value = await storage.getItem(key);
    if (value) bundle.storage[key] = value;
  }
  return isMoveable(bundle.secure, bundle.storage) ? bundle : null;
}

/** A staged bundle as the host returns it, or null for anything that is not one. */
export function parseHandover(text: unknown): HandoverBundle | null {
  if (typeof text !== "string") return null;
  let value: any;
  try {
    value = JSON.parse(text);
  } catch {
    return null;
  }
  if (value?.version !== 1 || !isStrings(value.secure) || !isStrings(value.storage)) return null;
  const secure: Record<string, string> = {};
  const storage: Record<string, string> = {};
  for (const key of DEVICE_SECURE_KEYS) if (value.secure[key]) secure[key] = value.secure[key];
  for (const key of STORAGE_KEYS) if (value.storage[key]) storage[key] = value.storage[key];
  return isMoveable(secure, storage) ? { version: 1, secure, storage } : null;
}

/** Paired: it has a device id and seed, and at least one computer. */
function isMoveable(secure: Record<string, string>, storage: Record<string, string>): boolean {
  const [id, , seed] = DEVICE_SECURE_KEYS;
  return Boolean(secure[id] && secure[seed] && storage[SERVER_KEYS.servers]);
}

function isStrings(value: unknown): value is Record<string, string> {
  return typeof value === "object" && value !== null && Object.values(value).every((item) => typeof item === "string");
}

/**
 * The new app takes what the old one staged, into a store that holds no device key and no computer (a fresh
 * install). An app that is already paired keeps what it has: the person scanned first, and that is the truth.
 * "imported": the phone is paired as it was; "kept": it was paired already.
 */
export async function importHandover(bundle: HandoverBundle, secure: KeyValueStore, storage: KeyValueStore): Promise<"imported" | "kept"> {
  const [id] = DEVICE_SECURE_KEYS;
  if ((await secure.getItem(id)) || (await storage.getItem(SERVER_KEYS.servers))) return "kept";
  for (const [key, value] of Object.entries(bundle.secure)) await secure.setItem(key, value);
  for (const [key, value] of Object.entries(bundle.storage)) await storage.setItem(key, value);
  return "imported";
}
