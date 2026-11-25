import { bodyPurpose, signedJSON } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";

/**
 * Anonymous usage counts, which the current computer sends (daemon/usage): its switch, and
 * this app's once-a-day count, which goes through the computer rather than to any server
 * of the app's own. Never content: the computer refuses anything outside its schema.
 */
export interface UsageState {
  enabled: boolean;
  /** What keeps counts off whatever the switch says ("environment", "ci", "development"); "" when the switch decides. */
  locked: string;
}

function parse(raw: Partial<UsageState> | null): UsageState {
  return { enabled: raw?.enabled === true, locked: typeof raw?.locked === "string" ? raw.locked : "" };
}

export async function getUsage(server: StoredServer): Promise<UsageState> {
  return parse(await signedJSON<Partial<UsageState>>(server, "GET", "/usage", "mewla-usage:get:GET:/usage"));
}

export async function setUsage(server: StoredServer, enabled: boolean): Promise<UsageState> {
  return parse(await signedJSON<Partial<UsageState>>(server, "POST", "/usage", bodyPurpose("mewla-usage:post:POST:/usage"), { body: { action: "set", enabled } }));
}

/** This app's day as the computer counts it: a random app id, the platform, the OS's major version and the app's. */
export interface AppDay {
  app: string;
  platform: string;
  os_major: string;
  version: string;
}

export async function sendAppDaily(server: StoredServer, day: AppDay): Promise<void> {
  await signedJSON(server, "POST", "/usage", bodyPurpose("mewla-usage:post:POST:/usage"), { body: { action: "app_daily", ...day } });
}

/** The platform as the usage schema names it. */
export function usagePlatform(os: string): string {
  if (os === "macos") return "mac";
  return os === "ios" || os === "android" || os === "web" ? os : "other";
}

/** The leading number of an OS version ("17.5" → "17", Android's API level "35" → "35"); "0" when there is none. */
export function osMajor(version: string): string {
  const match = /^(\d{1,2})(?:\D|$)/.exec(version.trim());
  return match ? match[1] : "0";
}

/** A random app id: 16 bytes as 32 hex. */
export function appIdFrom(random: Uint8Array): string {
  return Array.from(random.slice(0, 16), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

/** Whether `id` is an app id the computer accepts. */
export function validAppId(id: string | null | undefined): id is string {
  return typeof id === "string" && /^[0-9a-f]{32}$/.test(id);
}

/** Today as the computer's schema counts days (UTC). */
export function usageDay(now: Date): string {
  return now.toISOString().slice(0, 10);
}
