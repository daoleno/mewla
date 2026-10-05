import { getOrCreateLocalDeviceIdentity } from "./auth";
import { normalizeServerURL, type StoredServerInput } from "./storedServerContract";

export interface BrowserEnrollmentPrompt {
  requestId: string;
  secret: string;
  verificationNumber: string;
  expiresAt: string;
}

export interface BrowserEnrollmentStatus {
  status: "pending" | "approved" | "denied" | "expired";
  daemonId?: string;
  daemonPublicKey?: string;
  deviceId?: string;
}

export async function requestBrowserEnrollment(origin: string, fetcher: typeof fetch = fetch): Promise<BrowserEnrollmentPrompt> {
  const identity = await getOrCreateLocalDeviceIdentity();
  const base = normalizeOrigin(origin);
  const response = await fetcher(`${base}/enrollment/request`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ device_id: identity.deviceId, device_name: identity.deviceName, platform: "web", origin: base, device_public_key: identity.publicKeyHex }) });
  if (!response.ok) throw new Error("Zen is waiting for approval, but the request could not be created.");
  return response.json() as Promise<BrowserEnrollmentPrompt>;
}

export async function readBrowserEnrollmentStatus(origin: string, prompt: BrowserEnrollmentPrompt, fetcher: typeof fetch = fetch): Promise<BrowserEnrollmentStatus> {
  const response = await fetcher(`${normalizeOrigin(origin)}/enrollment/status?id=${encodeURIComponent(prompt.requestId)}&secret=${encodeURIComponent(prompt.secret)}`);
  if (!response.ok) throw new Error("The enrollment request is no longer available.");
  return response.json() as Promise<BrowserEnrollmentStatus>;
}

export function browserEnrollmentServer(origin: string, status: BrowserEnrollmentStatus): StoredServerInput | null {
  if (status.status !== "approved" || !status.daemonId || !status.daemonPublicKey) return null;
  const normalized = normalizeServerURL(origin);
  if (!normalized) return null;
  return { name: "Zen browser", url: normalized, daemonId: status.daemonId, daemonPublicKey: status.daemonPublicKey, transportKind: "manual" };
}

function normalizeOrigin(value: string): string {
  const parsed = new URL(value.trim());
  if (parsed.protocol !== "https:" && !(parsed.protocol === "http:" && (parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost"))) throw new Error("Zen enrollment requires HTTPS.");
  parsed.pathname = ""; parsed.search = ""; parsed.hash = "";
  return parsed.toString().replace(/\/$/, "");
}
