/**
 * Enrollment: a browser asks the computer to be let in (it shows a 3-digit number), and the
 * computer (`mewla devices approve NNN`) or a paired phone approves it. From the Expo app's
 * services/browserEnrollment.ts, enrollmentAPI.ts and enrollmentApproval.ts.
 */
import { getOrCreateLocalDeviceIdentity } from "./auth";
import { signedFetch } from "./more/daemon";
import { platform } from "./platform";
import { normalizeServerURL, type StoredServer, type StoredServerInput } from "./storedServerContract";

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

/** The page's origin as the computer's address: https, or http on this computer only. */
export function enrollmentOrigin(value: string): string {
  const parsed = new URL(value.trim());
  if (parsed.protocol !== "https:" && !(parsed.protocol === "http:" && (parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost"))) {
    throw new Error("Mewla enrollment requires HTTPS.");
  }
  return `${parsed.protocol}//${parsed.host}`;
}

/** Asks the computer at `origin` to let this browser in. */
export async function requestBrowserEnrollment(origin: string): Promise<BrowserEnrollmentPrompt> {
  const identity = await getOrCreateLocalDeviceIdentity();
  const base = enrollmentOrigin(origin);
  const response = await platform().fetch(`${base}/enrollment/request`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ device_id: identity.deviceId, device_name: identity.deviceName, platform: "web", origin: base, device_public_key: identity.publicKeyHex }),
  });
  if (!response.ok) throw new Error("Mewla is waiting for approval, but the request could not be created.");
  const raw = (await response.json()) as Record<string, unknown>;
  const number = String(raw.verification_number ?? "");
  if (typeof raw.request_id !== "string" || typeof raw.secret !== "string" || !/^\d{3}$/.test(number) || !Number.isFinite(Date.parse(String(raw.expires_at)))) {
    throw new Error("Invalid enrollment response.");
  }
  return { requestId: raw.request_id, secret: raw.secret, verificationNumber: number, expiresAt: String(raw.expires_at) };
}

export async function readBrowserEnrollmentStatus(origin: string, prompt: BrowserEnrollmentPrompt): Promise<BrowserEnrollmentStatus> {
  const url = `${enrollmentOrigin(origin)}/enrollment/status?id=${encodeURIComponent(prompt.requestId)}&secret=${encodeURIComponent(prompt.secret)}`;
  const response = await platform().fetch(url);
  if (!response.ok) throw new Error("The enrollment request is no longer available.");
  const raw = (await response.json()) as Record<string, unknown>;
  const status = raw.status === "approved" || raw.status === "denied" || raw.status === "expired" ? raw.status : "pending";
  return {
    status,
    daemonId: typeof raw.daemon_id === "string" ? raw.daemon_id : undefined,
    daemonPublicKey: typeof raw.daemon_public_key === "string" ? raw.daemon_public_key : undefined,
    deviceId: typeof raw.device_id === "string" ? raw.device_id : undefined,
  };
}

/** The computer to save once approved, named after its address (never the browser's). */
export function browserEnrollmentServer(origin: string, status: BrowserEnrollmentStatus): StoredServerInput | null {
  if (status.status !== "approved" || !status.daemonId || !status.daemonPublicKey) return null;
  const normalized = normalizeServerURL(origin);
  if (!normalized) return null;
  return { name: new URL(enrollmentOrigin(origin)).hostname, url: normalized, daemonId: status.daemonId, daemonPublicKey: status.daemonPublicKey, transportKind: "manual" };
}

export interface PendingEnrollment {
  id: string;
  deviceName: string;
  platform: string;
  origin: string;
  verificationNumber: string;
  expiresAt: string;
}

export function decodePendingEnrollment(raw: Record<string, unknown>): PendingEnrollment | null {
  const id = raw.request_id ?? raw.id;
  const number = raw.verification_number ?? raw.number;
  if (typeof id !== "string" || typeof number !== "string" || !/^\d{3}$/.test(number) || typeof raw.expires_at !== "string") return null;
  return { id, verificationNumber: number, deviceName: String(raw.device_name || "New device"), platform: String(raw.platform || "unknown"), origin: String(raw.origin || "Unknown address"), expiresAt: raw.expires_at };
}

/**
 * Four numbers to pick from: the right one and three decoys, shuffled, so the answer can't be
 * told by its place or by its distance from the others.
 */
export function verificationChoices(number: string, random: () => number = Math.random): string[] {
  const normalized = number.trim();
  if (!/^\d{3}$/.test(normalized)) return [];
  const choices = [normalized];
  const pool = Array.from({ length: 1000 }, (_, value) => String(value).padStart(3, "0")).filter((value) => value !== normalized);
  for (let index = 0; index < 3; index++) choices.push(pool.splice(Math.floor(random() * pool.length), 1)[0]!);
  for (let index = choices.length - 1; index > 0; index--) {
    const selected = Math.floor(random() * (index + 1));
    [choices[index], choices[selected]] = [choices[selected]!, choices[index]!];
  }
  return choices;
}

const DECISION_PURPOSE = "mewla-enrollment:decision:POST:/enrollment/decision";

/** The requests waiting on the current computer. */
export async function pendingEnrollments(server: StoredServer): Promise<PendingEnrollment[]> {
  const response = await signedFetch(server, "GET", "/enrollment/pending", DECISION_PURPOSE);
  if (!response.ok) throw new Error("Could not load pending requests.");
  const raw = (await response.json()) as { requests?: Record<string, unknown>[] };
  return (raw.requests ?? []).filter((r) => r.status === "pending").map(decodePendingEnrollment).filter((r): r is PendingEnrollment => r !== null);
}

/** Approves with the number the person picked, or denies. */
export async function decideEnrollment(server: StoredServer, request: PendingEnrollment, number: string, approve: boolean): Promise<void> {
  const response = await signedFetch(server, "POST", "/enrollment/decision", DECISION_PURPOSE, { body: { request_id: request.id, verification_number: number, approve } });
  if (response.ok) return;
  const detail = await response.text().catch(() => "");
  if (response.status === 409 && detail.includes("number")) throw new Error("That number doesn’t match. Check the new device and try again.");
  throw new Error(response.status === 409 ? "This request is no longer pending." : "Could not send the decision. Please try again.");
}
